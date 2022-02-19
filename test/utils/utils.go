package utils

import (
	"crypto/rand"
	"io/ioutil"
	"math/big"
	"os"
	"path"
	"strings"
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

// RandLowerCaseLetterString returns a lowercase letter string of given length
func RandLowerCaseLetterString(length int) string {
	chars := []rune("abcdefghijklmnopqrstuvwxyz")
	var b strings.Builder
	for i := 0; i < length; i++ {
		i, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b.WriteRune(chars[i.Int64()])
	}
	return b.String()
}
