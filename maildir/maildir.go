package maildir

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Maildir struct {
	path   string
	parent *Maildir
}

func Open(path string) (*Maildir, error) {
	path = filepath.Clean(path)

	for _, subdir := range []string{"tmp", "new", "cur"} {
		subdirPath := filepath.Join(path, subdir)
		fi, err := os.Stat(subdirPath)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("%s: not a directory", subdirPath)
		}
	}

	_, err := os.Stat(filepath.Join(path, "maildirfolder"))
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("maildirfolder present")
	}

	return &Maildir{path: path}, nil
}

func (md *Maildir) AddFolder(folder string) (*Maildir, error) {
	var path string
	if md.parent == nil {
		path = filepath.Join(md.path, "." + folder)
	} else {
		path = md.path + "." + folder
	}
	err := os.Mkdir(path, 0700)
	if err != nil && !os.IsExist(err) {
		return nil, err
	}

	for _, subdir := range []string{"tmp", "new", "cur"} {
		err = os.Mkdir(filepath.Join(path, subdir), 0700)
		if err != nil && !os.IsExist(err) {
			return nil, err
		}
	}

	f, err := os.OpenFile(filepath.Join(path, "maildirfolder"), os.O_CREATE, 0600)
	if err != nil && !os.IsExist(err) {
		return nil, err
	}
	f.Close()

	return &Maildir{
		path:   path,
		parent: md,
	}, nil
}

func (md *Maildir) AddMail(r io.Reader) error {
	// Not adhering Qmail's how a message is delivered page,
	// since most of it seems rather pointless.
	// TODO: check if using Dovecot's format is better
	//       cf maildir_filename_generate in
	//         lib-storage/index/maildir/maildir-filename.c
	tmp, err := os.CreateTemp(filepath.Join(md.path, "tmp"), "zimbridge-mda")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	_, err = io.Copy(tmp, r)
	if err != nil {
		return err
	}

	filename := filepath.Base(tmp.Name())
	err = os.Link(tmp.Name(), filepath.Join(md.path, "new", filename))
	if err != nil {
		return err
	}

	return nil
}
