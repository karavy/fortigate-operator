package vyos

import (
	"context"
	"fmt"
	"github.com/ganawaj/go-vyos/vyos"
)

func SendCommands(client *vyos.Client, command string) error {
	ctx := context.Background()

	out, resp, err := client.Conf.Set(ctx, command)
	if err != nil {
		return err
	}

	fmt.Printf("Response: %v\n", resp)
	fmt.Printf("Data: %v\n", out.Data)

	fmt.Println(out.Success)

	return nil
}