package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/jedib0t/go-pretty/v6/progress"
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

func Test_ip2int(t *testing.T) {
	i := ip2int(net.IPv4(0, 0, 0, 0))
	assert.Equal(t, uint32(0), i)
	i = ip2int(net.IPv4(255, 255, 255, 255))
	assert.Equal(t, uint32(0xFFFFFFFF), i)
}

func Test_initProgressBar(t *testing.T) {
	pb := initProgressBar()
	assert.IsType(t, &progress.Progress{}, pb)
}

func Test_buildDeviceURL(t *testing.T) {
	url := buildDeviceURL("testhost")
	assert.Equal(t, "http://testhost/cm?cmnd=Status%200", url)
}

func Test_parseFirmwareVersion(t *testing.T) {
	assert := assert.New(t)
	version, variant, err := parseFirmwareVersion("9.1.0(tasmota)")
	assert.Nil(err)
	assert.Equal("9.1.0", version)
	assert.Equal("tasmota", variant)
	version, variant, err = parseFirmwareVersion("test")
	assert.NotNil(err)
	assert.Empty(version)
	assert.Empty(variant)
}

func Test_checkDeviceVersion(t *testing.T) {
	assert := assert.New(t)
	var testDevice tasmoDevice
	testDevice.FirmwareVersion = "1.0.1"
	vlow, err := version.NewVersion("0.0.1")
	assert.Nil(err)
	vhigh, err := version.NewVersion("999.0.1")
	assert.Nil(err)
	vequal, err := version.NewVersion("1.0.1")
	assert.Nil(err)
	outDevice := checkDeviceVersion(vlow, testDevice)
	assert.Equal(false, outDevice.Outdated)
	outDevice = checkDeviceVersion(vhigh, testDevice)
	assert.Equal(true, outDevice.Outdated)
	outDevice = checkDeviceVersion(vequal, testDevice)
	assert.Equal(false, outDevice.Outdated)
}

func Test_getCurrentTasmotaVersion(t *testing.T) {
	v := getCurrentTasmotaVersion()
	assert.IsType(t, &version.Version{}, v)
}

// func Test_getDeviceData(t *testing.T) {
// 	assert := assert.New(t)
// 	ip := net.IPv4(127, 0, 0, 1)
// 	// old := getURL
// 	// defer func() { getURL = old }()
// 	var data = func() string {
// 		return deviceData
// 	}
// 	d, err := getDeviceData(ip)
// 	assert.Nil(err)
// 	assert.Equal("9.1.0", d.FirmwareVersion)
// 	assert.Equal(d.IP, net.IPv4(127, 0, 0, 1))

// }

func Test_getURL(t *testing.T) {
	assert := assert.New(t)
	srv := serverMock()
	defer srv.Close()
	urlData, err := getURL(srv.URL)
	assert.Nil(err)
	assert.Equal(deviceData, urlData)
}

func serverMock() *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, deviceData)
	}))
	return srv
}
