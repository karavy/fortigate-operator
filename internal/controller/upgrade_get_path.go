package controller

// scraper.go - Recupera il percorso di upgrade da Fortinet Upgrade Path Tool
//
// Uso: go run scraper.go <model> <versione_iniziale> <versione_finale>
// Es:  go run scraper.go FF181F 7.6.1 8.0.0
//
// Il cookie di sessione viene generato automaticamente tramite chromedp.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	baseAPIURL      = "https://docs.fortinet.com/upgrade-tool/upgrade-path"
	upgradeToolURL  = "https://docs.fortinet.com/upgrade-tool/fortigate"
	releaseNotesURL = "https://docs.fortinet.com/document/fortigate"
	productSlug     = "fortigate"
)

// ─── Strutture risposta API Fortinet ─────────────────────────────────────────

type Permalink struct {
	Slug        string `json:"slug"`
	PermanentID int    `json:"permanent_id"`
}

type VersionDetail struct {
	Version     string      `json:"version"`
	BuildNumber string      `json:"build_number"`
	Type        string      `json:"type"`
	Permalinks  []Permalink `json:"permalinks"`
}

type FortinetResponse struct {
	Result struct {
		Path             []VersionDetail `json:"path"`
		AvailableTo      []string        `json:"available_to"`
		AvailableFrom    []string        `json:"available_from"`
		AvailableToExt   []VersionDetail `json:"available_to_extended"`
		AvailableFromExt []VersionDetail `json:"available_from_extended"`
	} `json:"result"`
}

// ─── Ottieni cookie con chromedp ──────────────────────────────────────────────

func getSessionCookie() (string, error) {
	fmt.Println("Ottenimento cookie di sessione tramite browser headless...")

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.ExecPath("/usr/bin/google-chrome-stable"),
		chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Naviga sulla pagina — questo genera il cookie di sessione
	err := chromedp.Run(ctx,
		chromedp.Navigate(upgradeToolURL),
		chromedp.WaitReady("body"),
		// Aspetta che la pagina carichi il tool
		chromedp.Sleep(2*time.Second),
	)
	if err != nil {
		return "", fmt.Errorf("errore navigazione: %w", err)
	}

	// Recupera tutti i cookie
	var cookies []*network.Cookie
	err = chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().Do(ctx)
			return err
		}),
	)
	if err != nil {
		return "", fmt.Errorf("errore recupero cookie: %w", err)
	}

	// Cerca cookiesession1
	for _, c := range cookies {
		if c.Name == "cookiesession1" {
			fmt.Printf("  ✓ Cookie ottenuto: %s=%s\n\n", c.Name, c.Value)
			return fmt.Sprintf("%s", c.Value), nil
		}
	}

	// Debug: stampa tutti i cookie ricevuti
	fmt.Println("  Cookie ricevuti (debug):")
	for _, c := range cookies {
		fmt.Printf("    %s = %.20s...\n", c.Name, c.Value)
	}

	return "", fmt.Errorf("cookiesession1 non trovato tra i cookie della pagina")
}

// ─── HTTP client ──────────────────────────────────────────────────────────────

var httpClient = &http.Client{Timeout: 15 * time.Second}

// ─── Fetch upgrade path ───────────────────────────────────────────────────────

func fetchUpgradePath(model, from, to, cookie string) (*FortinetResponse, error) {
	formData := url.Values{}
	formData.Set("product_slug", "fortigate")
	formData.Set("model", model)
	formData.Set("current_version", from)
	formData.Set("target_version", to)

	body := formData.Encode()
	//fmt.Printf("DEBUG body: %s\n", body)
	//fmt.Printf("DEBUG cookie: %s\n", cookie)

	req, err := http.NewRequest("POST", "https://docs.fortinet.com/upgrade-tool/upgrade-path", strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", fmt.Sprintf("https://docs.fortinet.com/upgrade-tool/fortigate?model=%s&from=%s&to=%s", model, from, to))
	req.Header.Set("Origin", "https://docs.fortinet.com")
	req.Header.Set("Cookie", fmt.Sprintf("cookiesession1=%s", cookie))
	req.ContentLength = int64(len(body))

	// Stampa tutti gli header
	//fmt.Println("DEBUG headers:")
	//for k, v := range req.Header {
	//    fmt.Printf("  %s: %s\n", k, v)
	//}

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Printf("DEBUG errore HTTP Fortigate portal: %v\n", err)
		return nil, fmt.Errorf("errore HTTP: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("DEBUG status: %d\n", resp.StatusCode)
		return nil, fmt.Errorf("errore HTTP: status %d", resp.StatusCode)
	}

	var result FortinetResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		fmt.Printf("DEBUG response: %s\n", string(rawBody))
		return nil, fmt.Errorf("errore parsing JSON: %w", err)
	}
	return &result, nil
}

// ─── Permalink helper ─────────────────────────────────────────────────────────

func findPermalink(v VersionDetail, slug string) string {
	for _, p := range v.Permalinks {
		if p.Slug == slug {
			return fmt.Sprintf("%s/%s/%d/%s", releaseNotesURL, v.Version, p.PermanentID, slug)
		}
	}
	return ""
}

// ─── Normalizza versione ──────────────────────────────────────────────────────

func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	v = strings.Split(v, ",")[0]
	return strings.TrimSpace(v)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func getUpgradePath(model, from, to string) ([]VersionDetail, error) {
	model = normalizeVersion(model)
	from = normalizeVersion(from)
	to = normalizeVersion(to)

	ppath, perr := exec.LookPath("google-chrome-stable")
	if perr != nil {
		fmt.Println(perr)
		// non trovato nel PATH
		return nil, perr
	}
	fmt.Println(ppath)

	fmt.Printf("Fortinet Upgrade Path Finder\n")
	fmt.Printf("Model  : %s\n", model)
	fmt.Printf("From   : %s\n", from)
	fmt.Printf("To     : %s\n\n", to)

	// ── Step 1: ottieni cookie ────────────────────────────────────────────────
	cookie, err := getSessionCookie()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERRORE cookie: %v\n", err)
		return nil, err
	}

	// ── Step 2: fetch upgrade path ────────────────────────────────────────────
	fmt.Println("Recupero percorso di aggiornamento...")
	result, err := fetchUpgradePath(model, from, to, cookie)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERRORE fetch: %v\n", err)
		return nil, err
	}

	path := result.Result.Path
	if len(path) == 0 {
		fmt.Println("⚠  Nessun percorso trovato.")
		fmt.Println("   Versioni raggiungibili direttamente:")
		for _, v := range result.Result.AvailableTo {
			fmt.Printf("  → %s\n", v)
		}
		return nil, nil
	}

	// ── Step 3: stampa percorso ───────────────────────────────────────────────
	/*fmt.Printf("\nSequenza di aggiornamento (%d hop):\n\n", len(path)-1)
	for i, v := range path {
		switch {
		case i == 0:
			fmt.Printf("  [CURRENT] %s  (build %s, %s)\n", v.Version, v.BuildNumber, v.Type)
		case i == len(path)-1:
			fmt.Printf("  [TARGET]  %s  (build %s, %s)\n", v.Version, v.BuildNumber, v.Type)
		default:
			fmt.Printf("  [HOP %d]   %s  (build %s, %s)\n", i, v.Version, v.BuildNumber, v.Type)
		}
		if link := findPermalink(v, "upgrade-information"); link != "" {
			fmt.Printf("            📋 Upgrade info    : %s\n", link)
		}
		if link := findPermalink(v, "special-notices"); link != "" {
			fmt.Printf("            ⚠  Special notices : %s\n", link)
		}
		if i < len(path)-1 {
			fmt.Println("            ↓")
		}
	}*/

	// ── Step 4: output JSON ───────────────────────────────────────────────────
	/*fmt.Println()
	type PathEntry struct {
		Version string `json:"version"`
		Build   string `json:"build"`
		Type    string `json:"type"`
	}
	entries := make([]PathEntry, len(path))
	for i, v := range path {
		entries[i] = PathEntry{v.Version, v.BuildNumber, v.Type}
	}
	out := map[string]interface{}{
		"model": model,
		"from":  from,
		"to":    to,
		"hops":  len(path) - 1,
		"path":  entries,
	}
	jsonOut, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println("JSON:")
	fmt.Println(string(jsonOut))*/

	return path, nil
}
