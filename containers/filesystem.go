package containers

import (
	"fmt"
	"github.com/g8os/fs/config"
	"github.com/g8os/fs/files"
	"github.com/g8os/fs/meta"
	"github.com/g8os/fs/stor"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
)

const (
	PLIST_DIR               = "/tmp"
	BACKEND_BASE_DIR        = "/tmp"
	CONTAINER_BASE_ROOT_DIR = "/mnt"
)

func (*containerManager) getPlist(container uint64, src string) (string, error) {
	u, err := url.Parse(src)
	if err != nil {
		return "", err
	}

	if u.Scheme == "file" || u.Scheme == "" {
		// check file exists
		_, err := os.Stat(u.Path)
		if err != nil {
			return "", err
		}
		return u.Path, nil
	} else if u.Scheme == "http" || u.Scheme == "https" {
		response, err := http.Get(src)
		if err != nil {
			return "", err
		}

		defer response.Body.Close()
		name := path.Join(PLIST_DIR, fmt.Sprintf("container-%d.plist", container))

		file, err := os.Create(name)
		if err != nil {
			return "", err
		}
		defer file.Close()
		if _, err := io.Copy(file, response.Body); err != nil {
			return "", nil
		}

		return name, nil
	}

	return "", fmt.Errorf("invalid plist url %s", src)
}

func (c *containerManager) mountPList(container uint64, src string) (string, error) {
	//check
	plist, err := c.getPlist(container, src)
	if err != nil {
		return "", err
	}

	backend := path.Join(BACKEND_BASE_DIR, fmt.Sprintf("container-%d", container))
	target := path.Join(CONTAINER_BASE_ROOT_DIR, fmt.Sprintf("container-%d", container))

	os.RemoveAll(backend)
	os.RemoveAll(target)

	for _, p := range []string{backend, target} {
		if err := os.MkdirAll(p, 0755); err != nil {
			return "", fmt.Errorf("failed to create mount points '%s': %s", p, err)
		}
	}

	be := &config.Backend{Path: backend}

	if err := meta.PopulateFromPList(be, "/", plist, "/"); err != nil {
		return "", err
	}

	u, _ := url.Parse("ipfs://localhost:5001")

	store, err := stor.NewIPFSStor(u)
	if err != nil {
		return "", err
	}

	fs, err := files.NewFS(target, be, store,
		true, false)

	if err != nil {
		return "", err
	}

	go fs.Serve()

	fs.WaitMount()
	return target, nil
}