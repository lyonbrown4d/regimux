package object

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/afero"
)

type slashPathFS struct {
	source afero.Fs
}

func newSlashPathFS(source afero.Fs) afero.Fs {
	return slashPathFS{source: source}
}

func (fs slashPathFS) Create(name string) (afero.File, error) {
	file, err := fs.source.Create(slashPath(name))
	if err != nil {
		return nil, wrapError(err, "create slash path %s", name)
	}
	return file, nil
}

func (fs slashPathFS) Mkdir(name string, perm os.FileMode) error {
	if err := fs.source.Mkdir(slashPath(name), perm); err != nil {
		return wrapError(err, "create slash path directory %s", name)
	}
	return nil
}

func (fs slashPathFS) MkdirAll(name string, perm os.FileMode) error {
	if err := fs.source.MkdirAll(slashPath(name), perm); err != nil {
		return wrapError(err, "create slash path directory tree %s", name)
	}
	return nil
}

func (fs slashPathFS) Open(name string) (afero.File, error) {
	file, err := fs.source.Open(slashPath(name))
	if err != nil {
		return nil, wrapError(err, "open slash path %s", name)
	}
	return file, nil
}

func (fs slashPathFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := fs.source.OpenFile(slashPath(name), flag, perm)
	if err != nil {
		return nil, wrapError(err, "open slash path file %s", name)
	}
	return file, nil
}

func (fs slashPathFS) Remove(name string) error {
	if err := fs.source.Remove(slashPath(name)); err != nil {
		return wrapError(err, "remove slash path %s", name)
	}
	return nil
}

func (fs slashPathFS) RemoveAll(name string) error {
	if err := fs.source.RemoveAll(slashPath(name)); err != nil {
		return wrapError(err, "remove slash path tree %s", name)
	}
	return nil
}

func (fs slashPathFS) Rename(oldname, newname string) error {
	if err := fs.source.Rename(slashPath(oldname), slashPath(newname)); err != nil {
		return wrapError(err, "rename slash path %s", oldname)
	}
	return nil
}

func (fs slashPathFS) Stat(name string) (os.FileInfo, error) {
	info, err := fs.source.Stat(slashPath(name))
	if err != nil {
		return nil, wrapError(err, "stat slash path %s", name)
	}
	return info, nil
}

func (fs slashPathFS) Name() string {
	return fs.source.Name()
}

func (fs slashPathFS) Chmod(name string, mode os.FileMode) error {
	if err := fs.source.Chmod(slashPath(name), mode); err != nil {
		return wrapError(err, "chmod slash path %s", name)
	}
	return nil
}

func (fs slashPathFS) Chown(name string, uid, gid int) error {
	if err := fs.source.Chown(slashPath(name), uid, gid); err != nil {
		return wrapError(err, "chown slash path %s", name)
	}
	return nil
}

func (fs slashPathFS) Chtimes(name string, atime, mtime time.Time) error {
	if err := fs.source.Chtimes(slashPath(name), atime, mtime); err != nil {
		return wrapError(err, "change slash path times %s", name)
	}
	return nil
}

func slashPath(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

var _ afero.Fs = slashPathFS{}
