package main

import (
	"flag"
	"fmt"
	"httpc/pkg/libhttpserver"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func listFiles(path string) []string {
	allFiles, err := ioutil.ReadDir(path)
	files := []string{}
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range allFiles {
		if !file.IsDir() {
			files = append(files, file.Name())
		}
	}

	return files
}

func makeHeaders(responseBody string, responseHeaders []string) string {
	bodyLength := fmt.Sprintf("Content-Length:%d", len(responseBody))
	responseHeaders = append(responseHeaders, bodyLength)
	responseHeaders = append(responseHeaders, "Content-Disposition:inline")
	headers := strings.Join(responseHeaders, libhttpserver.CRLF)
	return headers
}

func getTypeHeader(fileType string) string {
	key := "Content-Type:"
	switch fileType {
	case ".txt":
		return key + "text/plain"
	case ".json":
		return key + "application/json"
	case ".xml":
		return key + "application/xml"
	case ".html":
		return key + "text/html"
	}
	return key + "text/plain"
}

func getHandler(reqData *libhttpserver.Request, pathParam *string, root *string) (string, int, string) {
	var fileMutex sync.Mutex

	if reqData.Method == "GET" {
		if pathParam == nil {
			files := listFiles(*root)
			body := strings.Join(files, ",")
			responseHeaders := makeHeaders(body, []string{})
			return body, 200, responseHeaders
		}

		if strings.Contains(*pathParam, "/") {
			errStr := fmt.Sprintf("Access Forbidden: '%s' is outside server root directory", *pathParam)
			return errStr, 403, makeHeaders(errStr, []string{})
		}

		fileMutex.Lock() // LOCK
		dat, err := ioutil.ReadFile(filepath.Join(*root, *pathParam))
		stringDat := string(dat)
		stringDat = strings.ReplaceAll(stringDat, "\r\n", "\n")
		getHeaders := makeHeaders(stringDat, []string{})
		ext := filepath.Ext(*pathParam)
		typeHeader := getTypeHeader(ext)
		getHeaders = getHeaders + libhttpserver.CRLF + typeHeader

		fileMutex.Unlock() // UNLOCK
		if err != nil {
			errStr := fmt.Sprintf("No file exists with name '%s'", *pathParam)
			return errStr, 404, makeHeaders(errStr, []string{})
		}
		return stringDat, 200, getHeaders
	} else if reqData.Method == "POST" {
		fileMutex.Lock() // LOCK
		err := ioutil.WriteFile(filepath.Join(*root, *pathParam), []byte(*reqData.Body), 0644)
		fileMutex.Unlock() // UNLOCK
		if err != nil {
			errStr := fmt.Sprintf("Failed to write to file '%s'", *pathParam)
			return errStr, 500, makeHeaders(errStr, []string{})
		} else {
			successStr := "Successfully written content to file"
			return successStr, 200, makeHeaders(successStr, []string{})
		}
	}
	return "", 500, makeHeaders("", []string{})
}

func parseArgs() {
	currDir, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}

	verbosePtr := flag.Bool("v", false, libhttpserver.HelpTextVerbose)
	dirPtr := flag.String("d", currDir, libhttpserver.HelpTextDir)
	portPtr := flag.String("p", "8080", libhttpserver.HelpTextPort)

	flag.Parse()
	fmt.Printf("Server listening on port: %s\nDirectory Served: %s\nVerbose Logging:%t\n\n", *portPtr, *dirPtr, *verbosePtr)

	PORT := ":" + *portPtr

	libhttpserver.RegisterHandler("POST", "/", getHandler)
	libhttpserver.RegisterHandler("GET", "/", getHandler)
	libhttpserver.StartServer(PORT, *dirPtr, *verbosePtr)
}

func main() {
	parseArgs()
}
