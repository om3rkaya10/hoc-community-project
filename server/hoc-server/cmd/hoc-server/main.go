package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/edge"
	"hoc-server/internal/gs"
	"hoc-server/internal/lobby"
)

func main() {
	root := config.RootDir()
	// Prefer parent Hoc if running from hoc-server/
	if filepath.Base(root) == "hoc-server" {
		parent := filepath.Dir(root)
		if _, err := os.Stat(filepath.Join(parent, "server.crt")); err == nil {
			_ = os.Chdir(parent)
			root = parent
		}
	}
	fmt.Println("============================================================")
	fmt.Println(" HOC Go Server (Pin+Go) — Nox-only migration target")
	fmt.Println(" CWD:", root)
	fmt.Println(" Ports: 80,443,8080,8443,9999,20001")
	fmt.Println("============================================================")

	if err := accounts.Load(config.AccountsPath()); err != nil {
		log.Fatalf("accounts: %v", err)
	}
	crt, key := config.CertPaths()

	go must(func() error { return edge.ListenHTTP(":80") })
	go must(func() error { return edge.ListenHTTP(":20001") }) // eve HTTP dump + discovery
	go must(func() error { return edge.ListenTLS(":443", crt, key) })
	go must(func() error { return edge.ListenTLS(":8443", crt, key) })
	go must(func() error {
		return edge.ServeDual8080(":8080", crt, key, lobby.Handle)
	})
	go must(func() error { return gs.Listen(":9999") })

	// also accept raw lobby if something connects plain after peek fail — covered by dual

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println(" shutting down...")
}

func must(fn func() error) {
	if err := fn(); err != nil {
		log.Printf(" listener error: %v", err)
	}
}
