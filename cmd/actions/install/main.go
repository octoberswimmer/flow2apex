package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	var repo string
	var version string
	var runnerOS string
	var runnerArch string
	var dest string

	flag.StringVar(&repo, "repo", "", "repository that hosts release assets")
	flag.StringVar(&version, "version", "", "release tag to download")
	flag.StringVar(&runnerOS, "runner-os", "", "runner operating system")
	flag.StringVar(&runnerArch, "runner-arch", "", "runner architecture")
	flag.StringVar(&dest, "dest", "", "destination directory for the flow2apex binary")
	flag.Parse()

	if repo == "" || version == "" {
		log.Fatal("both --repo and --version are required")
	}

	runnerOS = strings.TrimSpace(runnerOS)
	runnerArch = strings.TrimSpace(runnerArch)
	if runnerOS == "" {
		runnerOS = runtime.GOOS
	}
	if runnerArch == "" {
		runnerArch = runtime.GOARCH
	}

	platform, err := normalizeOS(runnerOS)
	if err != nil {
		log.Fatal(err)
	}
	arch, err := normalizeArch(platform, runnerArch)
	if err != nil {
		log.Fatal(err)
	}

	if dest == "" {
		log.Fatal("--dest must point to a writable directory (for example $RUNNER_TEMP/flow2apex)")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		log.Fatalf("create dest directory: %v", err)
	}

	archiveName := fmt.Sprintf("flow2apex_%s_%s_%s.zip", platform, arch, version)
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, version, archiveName)

	tmpDir, err := os.MkdirTemp("", "flow2apex-action-install-*")
	if err != nil {
		log.Fatalf("create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, archiveName)
	if err := downloadFile(url, archivePath); err != nil {
		log.Fatalf("download archive: %v", err)
	}

	extracted, err := extractFlow2ApexBinary(archivePath, tmpDir)
	if err != nil {
		log.Fatalf("extract flow2apex binary: %v", err)
	}

	binaryName := "flow2apex"
	if platform == "windows" {
		binaryName = "flow2apex.exe"
	}
	finalPath := filepath.Join(dest, binaryName)
	if err := moveFile(extracted, finalPath); err != nil {
		log.Fatalf("move binary: %v", err)
	}
	if platform != "windows" {
		if err := os.Chmod(finalPath, 0o755); err != nil {
			log.Fatalf("chmod binary: %v", err)
		}
	}

	pathFile := os.Getenv("GITHUB_PATH")
	if pathFile == "" {
		log.Fatal("GITHUB_PATH is not set")
	}
	if err := appendLine(pathFile, dest); err != nil {
		log.Fatalf("update GITHUB_PATH: %v", err)
	}

	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		log.Fatal("GITHUB_OUTPUT is not set")
	}
	if err := appendLine(outputFile, fmt.Sprintf("binary=%s", finalPath)); err != nil {
		log.Fatalf("write GITHUB_OUTPUT: %v", err)
	}

	fmt.Printf("Installed flow2apex binary to %s\n", finalPath)
}

func normalizeOS(osName string) (string, error) {
	switch strings.ToLower(osName) {
	case "linux":
		return "linux", nil
	case "macos", "darwin":
		return "darwin", nil
	case "windows":
		return "windows", nil
	default:
		return "", fmt.Errorf("unsupported operating system: %q", osName)
	}
}

func normalizeArch(platform, arch string) (string, error) {
	switch strings.ToLower(arch) {
	case "amd64", "x86_64", "x64":
		return "amd64", nil
	case "arm64", "aarch64":
		if platform == "windows" {
			return "", fmt.Errorf("windows arm64 is not supported by published flow2apex builds")
		}
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %q", arch)
	}
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected HTTP status %s from %s", resp.Status, url)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func extractFlow2ApexBinary(archivePath, destDir string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		if !strings.HasPrefix(base, "flow2apex") {
			continue
		}
		target := filepath.Join(destDir, base)
		if err := extractZipFile(f, target); err != nil {
			return "", err
		}
		return target, nil
	}
	return "", fmt.Errorf("flow2apex binary not found in archive")
}

func extractZipFile(file *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return err
	}
	if mode := file.Mode(); mode != 0 {
		if err := out.Chmod(mode); err != nil {
			return err
		}
	}
	return nil
}

func moveFile(src, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	if err := copyFile(src, dest); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}
