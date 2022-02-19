package utils

import (
	"io/ioutil"
	"os"
	"path"
	"time"
)

func CreateTmpDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	rootTmpPath := path.Join(cwd, "tmp")
	if _, err := os.Stat(rootTmpPath); os.IsNotExist(err) {
		if err := os.MkdirAll(rootTmpPath, 0755); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	dirNameFormat := time.Now().Format("2006-01-02-15-04-05-000000000-*")
	tmpPath, err := ioutil.TempDir(rootTmpPath, dirNameFormat)
	if err != nil {
		return "", err
	}

	return tmpPath, err
}
