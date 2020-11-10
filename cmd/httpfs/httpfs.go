package main

import (
	"flag"
	"fmt"
	"httpc/pkg/libhttpserver"
	"io/ioutil"
	"log"
	"os"
	"strings"
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
	headers := strings.Join(responseHeaders, libhttpserver.CRLF)
	return headers
}

func getHandler(reqData *libhttpserver.Request, pathParam *string, root *string) (string, int, string) {

	if reqData.Method == "GET" {
		if pathParam == nil {
			files := listFiles(*root)
			body := strings.Join(files, ",")
			responseHeaders := makeHeaders(body, []string{})
			return body, 200, responseHeaders
		}

		if strings.Contains(*pathParam, "/") {
			return "", 403, ""
		}

		dat, err := ioutil.ReadFile(*root + "\\" + *pathParam)
		if err != nil {
			return "", 404, ""
		}
		return string(dat), 200, ""
	} else if reqData.Method == "POST" {
		err := ioutil.WriteFile(*root+"\\"+*pathParam, []byte(*reqData.Body), 0644)
		if err != nil {
			return "", 200, ""
		}
	}
	return "", 200, ""
	//responseBody, responseHeaders :=
	//return responseBody, 200, responseHeaders
}

func parseArgs() {
	currDir, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}
	fmt.Println(currDir)

	verbosePtr := flag.Bool("v", false, libhttpserver.HelpTextVerbose)
	dirPtr := flag.String("d", currDir, libhttpserver.HelpTextDir)
	portPtr := flag.String("p", "8080", libhttpserver.HelpTextPort)

	flag.Parse()
	fmt.Println("v:", *verbosePtr)
	fmt.Println("d:", *dirPtr)
	fmt.Println("port:", *portPtr)

	PORT := ":" + *portPtr
	fmt.Println("PORT:", PORT)

	libhttpserver.RegisterHandler("POST", "/", getHandler)
	libhttpserver.RegisterHandler("GET", "/", getHandler)
	libhttpserver.StartServer(PORT, *dirPtr, *verbosePtr)
}

func main() {
	parseArgs()
}