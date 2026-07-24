package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, os.Args[1], nil)
	if err != nil {
		os.Exit(1)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		os.Exit(1)
	}
}
