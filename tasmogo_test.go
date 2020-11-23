package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

const deviceData = `
	{
		"Status": {
			"DeviceName": "Steckdose Schlafzimmer TV"
		}, 
		"StatusFWR": {
			"Version": "9.1.0(tasmota)"
		}
	}`

func TestGetDeviceFirmware(t *testing.T) {
	assert := assert.New(t)
	device, err := getDeviceFirmware(deviceData)
	assert.Nil(err)
	assert.Equal(device.FirmwareVersion, "9.1.0")
	assert.Equal(device.FirmwareType, "tasmota")
	assert.Equal(device.Name, "Steckdose Schlafzimmer TV")
}

func TestGetUrl(t *testing.T) {
	assert := assert.New(t)
	srv := serverMock()
	defer srv.Close()
	urlData, err := getUrl(srv.URL + "/cm")
	assert.Nil(err)
	assert.Equal(deviceData, urlData)
}

func serverMock() *httptest.Server {
	handler := http.NewServeMux()
	handler.HandleFunc("/cm", usersMock)
	srv := httptest.NewServer(handler)
	return srv
}

func usersMock(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(deviceData))
}
