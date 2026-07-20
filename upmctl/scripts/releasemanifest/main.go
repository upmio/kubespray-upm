package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const schemaVersion = "upmctl.release/v1"

type platform struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	ValidationTier string `json:"validationTier"`
}

type releaseFiles struct {
	Binary            string   `json:"binary"`
	Installer         string   `json:"installer"`
	InternalChecksums string   `json:"internalChecksums"`
	Readme            string   `json:"readme"`
	License           string   `json:"license"`
	Support           []string `json:"support"`
}

type manifest struct {
	SchemaVersion string       `json:"schemaVersion"`
	Product       string       `json:"product"`
	Version       string       `json:"version"`
	GitCommit     string       `json:"gitCommit"`
	BuildDate     string       `json:"buildDate"`
	APIVersion    string       `json:"apiVersion"`
	Platform      platform     `json:"platform"`
	Archive       string       `json:"archive"`
	ArchiveRoot   string       `json:"archiveRoot"`
	Files         releaseFiles `json:"files"`
}

func main() {
	var mode, source, output, version, commit, buildDate, goos, goarch, validationTier, archive string
	flag.StringVar(&mode, "mode", "", "generate or verify")
	flag.StringVar(&source, "source", "", "release package root")
	flag.StringVar(&output, "output", "", "manifest output for generate mode")
	flag.StringVar(&version, "version", "", "release version")
	flag.StringVar(&commit, "commit", "", "Git commit")
	flag.StringVar(&buildDate, "build-date", "", "UTC build date")
	flag.StringVar(&goos, "os", "", "target operating system")
	flag.StringVar(&goarch, "arch", "", "target architecture")
	flag.StringVar(&validationTier, "validation-tier", "", "platform validation tier")
	flag.StringVar(&archive, "archive", "", "archive file name")
	flag.Parse()

	if flag.NArg() != 0 || source == "" || version == "" || commit == "" || buildDate == "" || goos == "" || goarch == "" || validationTier == "" || archive == "" {
		fatalf("all release metadata flags and -source are required")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		fatalf("resolve source: %v", err)
	}
	expected, err := expectedManifest(source, version, commit, buildDate, goos, goarch, validationTier, archive)
	if err != nil {
		fatalf("build expected manifest: %v", err)
	}

	switch mode {
	case "generate":
		if output == "" {
			fatalf("-output is required in generate mode")
		}
		if err := writeManifest(output, expected); err != nil {
			fatalf("write manifest: %v", err)
		}
	case "verify":
		if output != "" {
			fatalf("-output is not accepted in verify mode")
		}
		if err := verifyManifest(filepath.Join(source, "release-manifest.json"), expected); err != nil {
			fatalf("verify manifest: %v", err)
		}
	default:
		fatalf("-mode must be generate or verify")
	}
}

func expectedManifest(source, version, commit, buildDate, goos, goarch, validationTier, archive string) (manifest, error) {
	support, err := supportFiles(source)
	if err != nil {
		return manifest{}, err
	}
	return manifest{
		SchemaVersion: schemaVersion,
		Product:       "upmctl",
		Version:       version,
		GitCommit:     commit,
		BuildDate:     buildDate,
		APIVersion:    "upmctl.upm.io/v1alpha1",
		Platform: platform{
			OS: goos, Arch: goarch, ValidationTier: validationTier,
		},
		Archive:     archive,
		ArchiveRoot: filepath.Base(source),
		Files: releaseFiles{
			Binary: "upmctl", Installer: "install.sh", InternalChecksums: "SHA256SUMS",
			Readme: "README.md", License: "LICENSE", Support: support,
		},
	}, nil
}

func supportFiles(source string) ([]string, error) {
	var files []string
	for _, directory := range []string{"docs", "scripts", "skills"} {
		root := filepath.Join(source, directory)
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("required support directory %s: %w", directory, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("required support path is not a directory: %s", directory)
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("support symlink is not allowed: %s", path)
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("unsupported support file type: %s", path)
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func writeManifest(output string, value manifest) (resultErr error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(output), ".release-manifest-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		if resultErr != nil {
			_ = os.Remove(tempName)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, 0o644); err != nil {
		return err
	}
	return os.Rename(tempName, output)
}

func verifyManifest(path string, expected manifest) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var actual manifest
	if err := decoder.Decode(&actual); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	actualJSON, _ := json.Marshal(actual)
	expectedJSON, _ := json.Marshal(expected)
	if string(actualJSON) != string(expectedJSON) {
		return fmt.Errorf("manifest content does not match release inputs")
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "release-manifest: "+strings.TrimSpace(format)+"\n", args...)
	os.Exit(2)
}
