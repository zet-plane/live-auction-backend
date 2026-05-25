package fs

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"

	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

func GetSize(f multipart.File) (int, error) {
	content, err := io.ReadAll(f)
	return len(content), err
}

func GetExt(fileName string) string {
	return path.Ext(fileName)
}

func FileExist(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		return os.IsExist(err)
	}
	return true
}

func CheckPermission(src string) bool {
	_, err := os.Stat(src)
	return os.IsPermission(err)
}

func IsNotExistMkDir(src string) error {
	if exist := FileExist(src); !exist {
		if err := MkDir(src); err != nil {
			return err
		}
	}
	return nil
}

func MkDir(src string) error {
	return os.MkdirAll(src, os.ModePerm)
}

func FileCreate(content bytes.Buffer, name string) {
	file, err := os.Create(name)
	if err != nil {
		logx.Error(err)
		return
	}
	defer file.Close()

	if _, err = file.WriteString(content.String()); err != nil {
		logx.Error(err)
	}
}

func Open(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

func GetType(p string) (string, error) {
	file, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buff := make([]byte, 512)
	if _, err = file.Read(buff); err != nil {
		return "", err
	}
	return http.DetectContentType(buff), nil
}
