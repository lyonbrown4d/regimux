package main

import "os"

type dockerContextSource interface {
	Open(name string) (*os.File, error)
}

type dockerContextDestination interface {
	MkdirAll(path string, perm os.FileMode) error
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
}

type dockerContextRemover interface {
	RemoveAll(path string) error
}

type dockerContextActivator interface {
	dockerContextRemover
	Rename(oldPath, newPath string) error
}
