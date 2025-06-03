package main

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

func main() {
	exist, err := pathExist(logPath)
	if err != nil {
		logrus.Fatalln(errors.Wrap(err, "pathExist"))
	}
	if !exist {
		err = os.MkdirAll(path.Dir(logPath), 0755)
		if err != nil {
			logrus.Fatalln(errors.Wrap(err, "os.MkdirAll"))
		}
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln(errors.Wrap(err, "os.OpenFile"))
	}

	defer func() {
		err = f.Close()
		if err != nil {
			logrus.Fatalln(errors.Wrap(err, "f.Close"))
		}
	}()

	multiWriter := io.MultiWriter(os.Stdout, f)

	logrus.SetOutput(multiWriter)
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05.000 Z07:00",
	})

	dir, err := getImgDir(saveDir)
	if err != nil {
		logrus.Fatalln(errors.Wrap(err, "getImgDir"))
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		logrus.Fatalln(errors.Wrap(err, "MkdirAll"))
	}

	type downloadTask struct {
		tileType tilesType
		index    int
		selector string
		isThumb  bool
		isOther  bool
		s        string
	}
	tasks := make(chan downloadTask, 100)
	var wg sync.WaitGroup

	// Start worker pool
	numWorkers := workers
	for i := 0; i < numWorkers; i++ {
		go func() {
			for task := range tasks {
				if task.isOther {
					if err := otherTilesDownloader(task.s, task.selector, saveDir, task.isThumb); err != nil {
						logrus.Error(errors.Wrap(err, "otherTilesDownloader"))
					}
					wg.Done()

				} else {
					if err := tilesDownloader(task.tileType, task.index, task.selector, saveDir, task.isThumb); err != nil {
						logrus.Error(errors.Wrap(err, "tilesDownloader"))
					}
					wg.Done()

				}
			}
		}()
	}

	// Send tasks to workers
	for t := range validTilesTypes {
		for i := tilesIndexStart[t]; i <= tilesIndexEnd[t]; i++ {
			for sel, isThumb := range selectorsThumb {
				wg.Add(1)
				tasks <- downloadTask{t, i, sel, isThumb, false, nullStr}
			}
		}
		for _, s := range otherTiles {
			for sel, isThumb := range selectorsThumb {
				wg.Add(1)
				tasks <- downloadTask{tilesTypeO, 0, sel, isThumb, true, s}
			}
		}
	}

	go func() {
		wg.Wait()
		close(tasks)
	}()

	wg.Wait()
	logrus.Infoln("All images downloaded successfully.")
}

type tilesType string

func (t tilesType) String() string {
	return string(t)
}

const (
	tilesTypeM tilesType = "m" // 万
	tilesTypeP tilesType = "p" // 筒
	tilesTypeS tilesType = "s" // 索
	tilesTypeZ tilesType = "z" // 字
	tilesTypeO tilesType = "other"
)

const (
	nullStr     = ""
	baseURL     = "http://wiki.lingshangkaihua.com/mediawiki/index.php/File:"
	extension   = ".png"
	saveDir     = "images"
	otherDir    = "other"
	logPath     = "logs/tiles_downloader.log"
	workers     = 4
	domain      = "http://wiki.lingshangkaihua.com"
	thumbPrefix = "thumb_"
)

var (
	validTilesTypes = map[tilesType]bool{
		tilesTypeM: true,
		tilesTypeP: true,
		tilesTypeS: true,
		tilesTypeZ: true,
	}

	tilesIndexEnd = map[tilesType]int{
		tilesTypeM: 9,
		tilesTypeP: 9,
		tilesTypeS: 9,
		tilesTypeZ: 7,
	}

	tilesIndexStart = map[tilesType]int{
		tilesTypeM: 0,
		tilesTypeP: 0,
		tilesTypeS: 0,
		tilesTypeZ: 1,
	}

	otherTiles = []string{
		"B",
	}

	selectorsThumb = map[string]bool{
		"#file > a > img": false,
		"#mw-imagepage-section-filehistory > table > tbody > tr:nth-child(2) > td:nth-child(3) > a > img": true,
	}
)

var (
	invalidTilesType  = errors.New("invalid tiles type")
	invalidTilesIndex = errors.New("invalid tiles index")
)

func getImgUrl(t tilesType, i int) (string, error) {
	if !validTilesTypes[t] {
		return nullStr, invalidTilesType
	}

	if i < tilesIndexStart[t] || i > tilesIndexEnd[t] {
		return nullStr, invalidTilesIndex
	}
	return fmt.Sprintf("%s%d%s%s", baseURL, i, t, extension), nil
}

func getOtherImgUrl(s string) (string, error) {
	return fmt.Sprintf("%s%s%s", baseURL, s, extension), nil
}

func getImgDir(dir string) (string, error) {
	abs, err := filepath.Abs(".")
	if err != nil {
		return nullStr, errors.Wrap(err, "filepath.Abs")
	}
	return strings.Replace(filepath.Join(abs, dir), string(os.PathSeparator), "/", -1), nil
}

func getImgName(t tilesType, i int, isThumb bool) (string, error) {
	if !validTilesTypes[t] {
		return nullStr, invalidTilesType
	}

	if i < tilesIndexStart[t] || i > tilesIndexEnd[t] {
		return nullStr, invalidTilesIndex
	}

	var prefix string

	if isThumb {
		prefix = thumbPrefix
	}
	return fmt.Sprintf("%s%d%s%s", prefix, i, t, extension), nil
}

func getOtherImgName(s string, isThumb bool) (string, error) {

	var prefix string

	if isThumb {
		prefix = thumbPrefix
	}

	return fmt.Sprintf("%s%s%s", prefix, s, extension), nil
}

func downloadImg(imgSrc string, dir string, name string) error {

	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrap(err, "os.MkdirAll")
	}

	url := fmt.Sprintf("%s%s", domain, imgSrc)

	resp, err := http.Get(url)
	if err != nil {
		return errors.Wrap(err, "http.Get")
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logrus.Infoln(err, "Body.Close")
		}
	}(resp.Body)

	filePath := strings.Replace(filepath.Join(dir, name), string(os.PathSeparator), "/", -1)
	file, err := os.Create(filePath)
	if err != nil {
		return errors.Wrap(err, "os.Create")
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logrus.Infoln(err, "file.Close")
		}
	}(file)

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return errors.Wrap(err, "io.Copy")
	}

	return nil
}

func tilesDownloader(t tilesType, i int, selector string, dirName string, isThumb bool) error {

	url, err := getImgUrl(t, i)
	if err != nil {
		return errors.Wrap(err, "getImgUrl")
	}

	resp, err := http.Get(url)
	if err != nil {
		return errors.Wrap(err, "http.Get")
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logrus.Infoln(err, "Body.Close")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("status code %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return errors.Wrap(err, "goquery.NewDocumentFromReader")
	}

	imgSrc, exists := doc.Find(selector).First().Attr("src")
	if !exists {
		return errors.Errorf("%s not found", selector)
	}

	fileName, err := getImgName(t, i, isThumb)
	if err != nil {
		return errors.Wrap(err, "getImgName")
	}

	dir, err := getImgDir(dirName)
	if err != nil {
		return errors.Wrap(err, "getImgDir")
	}

	subDir := strings.Replace(filepath.Join(dir, t.String()), string(os.PathSeparator), "/", -1)

	err = downloadImg(imgSrc, subDir, fileName)
	if err != nil {
		return errors.Wrap(err, "downloadImg")
	}

	logrus.Infof("Downloaded %s to %s/%s", imgSrc, subDir, fileName)

	return nil
}

func otherTilesDownloader(s string, selector string, dirName string, isThumb bool) error {
	url, err := getOtherImgUrl(s)
	if err != nil {
		return errors.Wrap(err, "getOtherImgUrl")
	}

	resp, err := http.Get(url)
	if err != nil {
		return errors.Wrap(err, "http.Get")
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logrus.Infoln(err, "Body.Close")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("status code %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return errors.Wrap(err, "goquery.NewDocumentFromReader")
	}

	imgSrc, exists := doc.Find(selector).First().Attr("src")
	if !exists {
		return errors.Errorf("%s not found", selector)
	}

	fileName, err := getOtherImgName(s, isThumb)
	if err != nil {
		return errors.Wrap(err, "getImgName")
	}

	dir, err := getImgDir(dirName)
	if err != nil {
		return errors.Wrap(err, "getImgDir")
	}

	subDir := strings.Replace(filepath.Join(dir, otherDir), string(os.PathSeparator), "/", -1)

	err = downloadImg(imgSrc, subDir, fileName)
	if err != nil {
		return errors.Wrap(err, "downloadImg")
	}

	logrus.Infof("Downloaded %s to %s/%s", imgSrc, subDir, fileName)

	return nil
}

func pathExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, errors.Wrap(err, "os.Stat")
}
