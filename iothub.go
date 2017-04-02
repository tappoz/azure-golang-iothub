package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"text/template"
	"time"
)

const (
	maxIdleConnections int    = 100
	requestTimeout     int    = 10
	tokenValidSecs     int    = 3600
	apiVersion         string = "2016-11-14"
)

// IoTHub representation
type IoTHub struct {
	HostName            string
	SharedAccessKeyName string
	SharedAccessKey     string
	Client              *http.Client
}

func NewIoTHub(conn string) (hub *IoTHub, err error) {
	// hijack the ParseQuery function to split the connection string into a map
	fields, err := url.ParseQuery(conn)
	if err != nil {
		log.Fatal(err)
	}
	hub = new(IoTHub)

	// use reflection to match each connection string component with a struct field
	// TODO: make sure we have all required fields
	t := reflect.ValueOf(hub).Elem()
	for k, v := range fields {
		val := t.FieldByName(k)
		val.Set(reflect.ValueOf(v[0]))
	}

	// set up a shared client for all connections, with long timeouts
	hub.Client = &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: maxIdleConnections,
		},
		Timeout: time.Duration(requestTimeout) * time.Second,
	}
	return hub, nil
}

func buildSasToken(hub *IoTHub, uri string) string {
	timestamp := time.Now().Unix() + int64(tokenValidSecs)
	encodedUri := template.URLQueryEscaper(uri)
	toSign := encodedUri + "\n" + strconv.FormatInt(timestamp, 10)
	binKey, _ := base64.StdEncoding.DecodeString(hub.SharedAccessKey)
	mac := hmac.New(sha256.New, []byte(binKey))
	mac.Write([]byte(toSign))
	encodedSignature := template.URLQueryEscaper(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return fmt.Sprintf("SharedAccessSignature sr=%s&sig=%s&se=%d&skn=%s", encodedUri, encodedSignature, timestamp, hub.SharedAccessKeyName)
}

// Perform individual requests (re-using session)
func performRequest(hub *IoTHub, method string, url string, data string) (string, string) {
	token := buildSasToken(hub, url)
	log.Printf("%s https://%s\n", method, url)
	req, _ := http.NewRequest(method, "https://"+url, bytes.NewBufferString(data))
	log.Println(data)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "golang-iot-client")
	req.Header.Set("Authorization", token)
	if method == "DELETE" {
		req.Header.Set("If-Match", "*")
	}

	resp, err := hub.Client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	// read the entire reply to ensure connection re-use
	text, _ := ioutil.ReadAll(resp.Body)
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
	return string(text), resp.Status
}

// CreateDeviceID adds a given device to an IoTHub and
// returns the HTTP request data
func CreateDeviceID(hub *IoTHub, deviceID string) (string, string) {
	url := fmt.Sprintf("%s/devices/%s?api-version=%s", hub.HostName, deviceID, apiVersion)
	data := fmt.Sprintf(`{"deviceId":"%s"}`, deviceID)
	return performRequest(hub, "PUT", url, data)
}

func GetDeviceID(hub *IoTHub, deviceID string) (string, string) {
	url := fmt.Sprintf("%s/devices/%s?api-version=%s", hub.HostName, deviceID, apiVersion)
	return performRequest(hub, "GET", url, "")
}

func DeleteDeviceID(hub *IoTHub, deviceID string) (string, string) {
	url := fmt.Sprintf("%s/devices/%s?api-version=%s", hub.HostName, deviceID, apiVersion)
	return performRequest(hub, "DELETE", url, "")
}

func PurgeCommandsForDeviceID(hub *IoTHub, deviceID string) (string, string) {
	url := fmt.Sprintf("%s/devices/%s/commands?api-version=%s", hub.HostName, deviceID, apiVersion)
	return performRequest(hub, "DELETE", url, "")
}

func ListDeviceIDs(hub *IoTHub, top int) (string, string) {
	url := fmt.Sprintf("%s/devices?top=%d&api-version=%s", hub.HostName, top, apiVersion)
	return performRequest(hub, "GET", url, "")
}

func SendMessage(hub *IoTHub, deviceID string, message string) (string, string) {
	url := fmt.Sprintf("%s/devices/%s/messages/events?api-version=%s", hub.HostName, deviceID, apiVersion)
	return performRequest(hub, "POST", url, message)
}

func main() {
	connectionString := os.Getenv("CONNECTION_STRING")
	if connectionString == "" {
		log.Fatal("No CONNECTION_STRING in environment")
	}
	hub, _ := NewIoTHub(connectionString)
	resp, status := ListDeviceIDs(hub, 10)
	log.Printf("%s, %s\n\n", resp, status)
	resp, status = CreateDeviceID(hub, "gopherTestDevice")
	log.Printf("%s, %s\n\n", resp, status)
	resp, status = GetDeviceID(hub, "gopherTestDevice")
	log.Printf("%s, %s\n\n", resp, status)
	resp, status = PurgeCommandsForDeviceID(hub, "gopherTestDevice")
	log.Printf("%s, %s\n\n", resp, status)
	for i := 0; i < 200; i++ {
		resp, status = SendMessage(hub, "gopherTestDevice", fmt.Sprintf(`{"deviceID":"%s", "count":%d}`, "gopherTestDevice", i))
	}
	log.Printf("%s, %s\n\n", resp, status)
	resp, status = DeleteDeviceID(hub, "gopherTestDevice")
	log.Printf("%s, %s\n\n", resp, status)
}
