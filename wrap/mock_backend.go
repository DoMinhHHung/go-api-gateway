package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func main1() {
	mux := http.NewServeMux()

	// Handle mọi request
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]string{
			"message": "Hello from Mock Backend",
			"method":  r.Method,
			"path":    r.URL.Path,
		}
		json.NewEncoder(w).Encode(response)
	})

	fmt.Println("Mock Backend đang chạy ở port 8081...")
	log.Fatal(http.ListenAndServe(":8081", mux))
}
