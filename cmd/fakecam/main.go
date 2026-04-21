// fakecam serves a raw Dahua multipart log file as an HTTP stream.
// Usage: go run ./cmd/fakecam [-port 9999] <raw-log-file>
// Add a camera entry pointing to http://localhost:9999/ to test the pipeline.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	port := flag.Int("port", 9999, "port to listen on")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: fakecam [-port N] <raw-log-file>")
		os.Exit(1)
	}

	logFile := flag.Arg(0)
	data, err := os.ReadFile(logFile)
	if err != nil {
		log.Fatalf("read %s: %v", logFile, err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("connection from %s", r.RemoteAddr)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=myboundary")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(data); err != nil {
			log.Printf("write error: %v", err)
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// keep connection open so the stream doesn't EOF immediately
		<-r.Context().Done()
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("fake camera serving %s on http://localhost%s/", logFile, addr)
	srv := &http.Server{Addr: addr, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
