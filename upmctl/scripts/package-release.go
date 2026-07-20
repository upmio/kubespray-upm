package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	var source, output, epochText string
	flag.StringVar(&source, "source", "", "source directory")
	flag.StringVar(&output, "output", "", "output tar.gz")
	flag.StringVar(&epochText, "epoch", "", "archive timestamp as Unix seconds")
	flag.Parse()
	if source == "" || output == "" || epochText == "" || flag.NArg() != 0 {
		fatalf("usage: package-release -source DIR -output FILE -epoch UNIX_SECONDS")
	}

	epoch, err := strconv.ParseInt(epochText, 10, 64)
	if err != nil || epoch < 0 {
		fatalf("invalid epoch %q", epochText)
	}
	stamp := time.Unix(epoch, 0).UTC()

	source, err = filepath.Abs(source)
	if err != nil {
		fatalf("resolve source: %v", err)
	}
	output, err = filepath.Abs(output)
	if err != nil {
		fatalf("resolve output: %v", err)
	}
	if rel, relErr := filepath.Rel(source, output); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		fatalf("output must not be inside source directory")
	}

	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		fatalf("source must be a readable directory: %v", err)
	}

	entries := make([]string, 0)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not permitted in release archives: %s", path)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported release entry: %s", path)
		}
		entries = append(entries, path)
		return nil
	})
	if err != nil {
		fatalf("walk source: %v", err)
	}
	sort.Strings(entries)

	if err := writeArchive(source, output, entries, stamp); err != nil {
		fatalf("create archive: %v", err)
	}
}

func writeArchive(source, output string, entries []string, stamp time.Time) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(output), ".upmctl-release-*.tmp")
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

	gz, err := gzip.NewWriterLevel(temp, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.Header.ModTime = time.Unix(0, 0).UTC()
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)

	root := filepath.Base(source)
	if err := writeDirHeader(tw, root+"/", stamp); err != nil {
		return err
	}
	for _, path := range entries {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(root, rel))
		if info.IsDir() {
			if err := writeDirHeader(tw, name+"/", stamp); err != nil {
				return err
			}
			continue
		}

		header := &tar.Header{
			Name: name, Mode: int64(info.Mode().Perm()), Size: info.Size(),
			ModTime: stamp, AccessTime: stamp, ChangeTime: stamp,
			Typeflag: tar.TypeReg, Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			Format: tar.FormatPAX,
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
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

func writeDirHeader(writer *tar.Writer, name string, stamp time.Time) error {
	return writer.WriteHeader(&tar.Header{
		Name: name, Mode: 0o755, ModTime: stamp, AccessTime: stamp, ChangeTime: stamp,
		Typeflag: tar.TypeDir, Uid: 0, Gid: 0, Uname: "root", Gname: "root",
		Format: tar.FormatPAX,
	})
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "package-release: "+format+"\n", args...)
	os.Exit(2)
}
