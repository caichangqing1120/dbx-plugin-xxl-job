// Command verify-package checks the actual delivered ZIP, not the build directory.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func main() {
	require(len(os.Args) > 1, "usage: go run scripts/verify-package.go FILE.dbxp ...")
	doHandshake := false
	for _, arg := range os.Args[1:] {
		if arg == "--handshake" {
			doHandshake = true
		}
	}
	for _, name := range os.Args[1:] {
		if name != "--handshake" {
			verify(name, doHandshake)
		}
	}
}
func verify(name string, doHandshake bool) {
	var metadata struct {
		Target, SHA256 string
		Size           int64
	}
	meta, err := os.ReadFile(strings.TrimSuffix(name, ".dbxp") + ".artifact.json")
	must(err)
	must(json.Unmarshal(meta, &metadata))
	archive, err := os.ReadFile(name)
	must(err)
	digest := sha256.Sum256(archive)
	require(hex.EncodeToString(digest[:]) == metadata.SHA256 && int64(len(archive)) == metadata.Size, "artifact checksum/size mismatch")
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	must(err)
	files := map[string][]byte{}
	modes := map[string]os.FileMode{}
	for _, f := range z.File {
		require(f.Name == path.Clean(f.Name) && !strings.HasPrefix(f.Name, "/") && !strings.Contains(f.Name, "..") && !strings.Contains(f.Name, "\\"), "unsafe archive path")
		_, exists := files[f.Name]
		require(!exists, "duplicate entry")
		require(f.Mode().IsRegular() && f.UncompressedSize64 < 64<<20, "invalid file type/size")
		r, e := f.Open()
		must(e)
		b, e := io.ReadAll(r)
		must(e)
		must(r.Close())
		files[f.Name] = b
		modes[f.Name] = f.Mode()
	}
	var checksums struct {
		Algorithm string
		Files     map[string]string
	}
	must(json.Unmarshal(files["checksums.json"], &checksums))
	require(checksums.Algorithm == "sha256" && len(checksums.Files)+1 == len(files), "checksum coverage mismatch")
	for p, expected := range checksums.Files {
		b, exists := files[p]
		require(exists, "missing entry")
		sum := sha256.Sum256(b)
		require(hex.EncodeToString(sum[:]) == expected, "entry checksum mismatch: "+p)
	}
	var manifest struct {
		ID, Version string
		Entrypoints struct{ Backend struct{ Executable string } }
	}
	must(json.Unmarshal(files["manifest.json"], &manifest))
	require(manifest.ID == "io.github.caichangqing1120.xxljob" && manifest.Version == "0.2.2", "plugin identity mismatch")
	exe := manifest.Entrypoints.Backend.Executable
	require(strings.HasPrefix(exe, "bin/"+metadata.Target+"/"), "target and executable path mismatch")
	require(len(files) == 12, "unexpected package entries")
	assets := map[string]bool{"assets/plugin.svg": true, "assets/THIRD_PARTY_NOTICES.txt": true, "assets/PLUGIN-LICENSE.txt": true, "assets/SDK-LICENSE.txt": true, "assets/VUE-LICENSE.txt": true, "assets/LUCIDE-LICENSE.txt": true, "assets/GO-NET-LICENSE.txt": true, "assets/GO-LICENSE.txt": true}
	for p := range files {
		require(p == exe || p == "checksums.json" || p == "manifest.json" || p == "ui/index.html" || assets[p], "unexpected entry: "+p)
	}
	binary, ok := files[exe]
	require(ok, "missing executable")
	switch metadata.Target {
	case "darwin-arm64", "darwin-x64":
		f, e := macho.NewFile(bytes.NewReader(binary))
		must(e)
		cpu := macho.CpuAmd64
		if metadata.Target == "darwin-arm64" {
			cpu = macho.CpuArm64
		}
		require(f.Cpu == cpu, "Mach-O CPU mismatch")
		require(modes[exe]&0o111 != 0, "missing Unix executable permission")
	case "windows-x64", "windows-arm64":
		require(strings.HasSuffix(exe, ".exe"), "Windows entry must have .exe suffix")
		f, e := pe.NewFile(bytes.NewReader(binary))
		must(e)
		machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if metadata.Target == "windows-arm64" {
			machine = pe.IMAGE_FILE_MACHINE_ARM64
		}
		require(f.Machine == machine, "PE CPU mismatch")
	case "linux-x64", "linux-arm64":
		f, e := elf.NewFile(bytes.NewReader(binary))
		must(e)
		machine := elf.EM_X86_64
		if metadata.Target == "linux-arm64" {
			machine = elf.EM_AARCH64
		}
		require(f.Machine == machine, "ELF CPU mismatch")
		require(modes[exe]&0o111 != 0, "missing Unix executable permission")
	default:
		panic("unsupported artifact target")
	}
	fmt.Printf("PASS %s: architecture, entrypoint, exact archive entries, SHA256 and metadata\n", metadata.Target)
	if doHandshake {
		handshake(binary)
	}
}

func handshake(binary []byte) {
	dir, err := os.MkdirTemp("", "xxljob-handshake-")
	must(err)
	defer os.RemoveAll(dir)
	executable := dir + "/xxljob"
	must(os.WriteFile(executable, binary, 0o700))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable)
	cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"plugin/initialize\",\"params\":{\"host\":{\"protocolVersions\":[1]}}}\n")
	output, err := cmd.Output()
	must(err)
	var reply struct {
		Result struct {
			ProtocolVersion int
			Plugin          struct{ ID, Version string }
		}
	}
	must(json.Unmarshal(bytes.TrimSpace(output), &reply))
	require(reply.Result.ProtocolVersion == 1 && reply.Result.Plugin.ID == "io.github.caichangqing1120.xxljob" && reply.Result.Plugin.Version == "0.2.2", "package handshake mismatch")
	fmt.Println("PASS packaged binary: plugin/initialize identity, version and protocol 1")
}
