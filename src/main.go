package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mdp/qrterminal/v3"
)

var version = "0.1.0"

var (
	flagPath    = flag.String("p", "", "carpeta a escanear (por defecto: junto al binario)")
	flagPort    = flag.String("port", "8420", "puerto del servidor")
	flagLibrary = flag.String("library", "", "identificador estable de biblioteca (útil para discos extraíbles)")
)

func main() {
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	base := binaryDir()
	root, err := resolveRoot(*flagPath, base)
	if err != nil {
		fatal("carpeta a escanear", err)
	}

	dataDir := root
	if strings.Trim(*flagPath, `"`) == "" {
		dataDir = base
	}
	dbPath := filepath.Join(dataDir, "gobby.db")
	db, err := openDB(dbPath)
	if err != nil {
		fatal("open db", err)
	}
	defer db.Close()
	libraryKey := *flagLibrary
	if libraryKey == "" {
		libraryKey = rootKey(root)
	}
	lb := &lib{root: root, key: libraryKey}

	go func() {
		if _, err := scan(db, root, libraryKey); err != nil {
			slog.Warn("scan error", "err", err)
			return
		}
		pending, err := pendingEnrichmentCount(db, libraryKey)
		if err != nil {
			slog.Warn("could not inspect pending enrichment", "err", err)
			return
		}
		if pending > 0 {
			if found, err := enrich(db, libraryKey, "all", false, true); err != nil {
				slog.Warn("automatic enrichment failed", "err", err)
			} else {
				slog.Info("automatic enrichment complete", "found", found)
			}
		}
	}()

	addr := "0.0.0.0:" + *flagPort
	printAccess(*flagPort, version)

	if err := serve(ctx, db, addr, version, dbPath, lb); err != nil {
		if strings.Contains(err.Error(), "Only one usage") || strings.Contains(err.Error(), "address already in use") {
			fatal("puerto ocupado", fmt.Errorf("el puerto %s ya está en uso — ¿tienes otro Gobby abierto? Ciérralo y vuelve a intentarlo", *flagPort))
		}
		fatal("serve", err)
	}
}

func resolveRoot(requested, base string) (string, error) {
	root := strings.Trim(requested, `"`)
	if root == "" {

		root = filepath.Dir(base)
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	if !dirExists(root) {
		return "", fmt.Errorf("%q no es una carpeta accesible", root)
	}
	return root, nil
}

func rootKey(root string) string {
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return strings.ToLower(filepath.Clean(root))
}

func printAccess(port, version string) {
	host := lanIP()
	if host == "" {
		host = "localhost"
	}
	url := fmt.Sprintf("http://%s:%s", host, port)

	fmt.Fprintf(os.Stderr, "\n  Gobby %s 🧦\n", version)
	fmt.Fprintf(os.Stderr, "  %s\n\n", url)
	qrterminal.GenerateHalfBlock(url, qrterminal.M, os.Stderr)
}

func lanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip.IsPrivate() {
				return ip.String()
			}
		}
	}
	return ""
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func binaryDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func fatal(what string, err error) {
	fmt.Fprintf(os.Stderr, "\n[gobby] ERROR (%s): %v\n", what, err)

	fmt.Fprintln(os.Stderr, "\nPulsa Enter para cerrar…")
	fmt.Fscanln(os.Stdin)
	os.Exit(1)
}
