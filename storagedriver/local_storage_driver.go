package storagedriver

import (
	"os"
)

type localStorageDriver struct{}

func (l *localStorageDriver) Exists(filepath string) bool {
	_, err := os.Stat(filepath)
	return err == nil
}

func NewLocalStorageDriver() StorageDriver {
	return &localStorageDriver{}
}
