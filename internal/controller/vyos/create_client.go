package vyos

import (
	"fmt"

	"github.com/ganawaj/go-vyos/vyos"
)

func InitVyosClient(hostname string, port string, apiKey string, insecure bool) (*vyos.Client, error) {
	url := fmt.Sprintf("https://%s:%s", hostname, port)

	client := vyos.NewClient(nil).WithToken(apiKey).WithURL(url)

	if client == nil {
		return nil, fmt.Errorf("failed to create vyos client")
	}

	if !insecure {
		client = client.Insecure()
	}

	return client, nil
}
