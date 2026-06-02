package docker

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/build"
)

// ManagedImage is the tag of the PgFleet managed-instance image
// (PostgreSQL + pgBackRest).
const ManagedImage = "pgfleet/postgres-pgbackrest:16"

// BuildImage builds an image from contextDir (which must contain a Dockerfile)
// and tags it. It streams the build and returns the first build error, if any.
func (m *Moby) BuildImage(ctx context.Context, contextDir, tag string) error {
	tarball, err := tarDir(contextDir)
	if err != nil {
		return fmt.Errorf("docker: tar build context: %w", err)
	}

	resp, err := m.cli.ImageBuild(ctx, tarball, build.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	if err != nil {
		return fmt.Errorf("docker: build %s: %w", tag, err)
	}
	defer resp.Body.Close()

	return drainBuildOutput(resp.Body)
}

// drainBuildOutput consumes the build JSON stream and surfaces a build error.
func drainBuildOutput(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var msg struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil && msg.Error != "" {
			return fmt.Errorf("docker: build failed: %s", msg.Error)
		}
	}
	return scanner.Err()
}

// tarDir streams a directory tree as a tar archive suitable for a build context.
func tarDir(dir string) (io.Reader, error) {
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = filepath.ToSlash(rel)
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		})
		_ = tw.Close()
		_ = pw.CloseWithError(err)
	}()
	return pr, nil
}
