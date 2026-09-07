// healthcheck — крошечная утилита для HEALTHCHECK scratch-образа
// (в scratch нет ни curl, ни python — только то, что положили сами)
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8356"
	}

	client := &http.Client{Timeout: 3 * time.Second}

	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		os.Exit(1)
	}
	resp.Body.Close()
}
