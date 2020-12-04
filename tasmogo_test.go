package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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
	url := buildDeviceURL("testhost", "")
	assert.Equal(t, "http://testhost/cm?cmnd=Status%200", url)
	url = buildDeviceURL("testhost", "test")
	assert.Equal(t, "http://testhost/cm?user=admin&password=test&cmnd=Status%200", url)
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
	outDevice, err := checkDeviceVersion(vlow, testDevice)
	assert.Nil(err)
	assert.Equal(false, outDevice.Outdated)
	outDevice, err = checkDeviceVersion(vhigh, testDevice)
	assert.Nil(err)
	assert.Equal(true, outDevice.Outdated)
	outDevice, err = checkDeviceVersion(vequal, testDevice)
	assert.Nil(err)
	assert.Equal(false, outDevice.Outdated)
	testDevice.FirmwareVersion = ""
	outDevice, err = checkDeviceVersion(vequal, testDevice)
	assert.NotNil(err)
}

func Test_getCurrentTasmotaVersion(t *testing.T) {
	v := getCurrentTasmotaVersion(versionData)
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

	urlData, err = getURL("test")
	assert.NotNil(err)
}

func serverMock() *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, deviceData)
	}))
	return srv
}

func Test_getPasswordQuery(t *testing.T) {
	auth := getPasswordQuery("test")
	assert.Equal(t, "user=admin&password=test&", auth)
}

func Test_renderDeviceTable(t *testing.T) {
	devices := []tasmoDevice{
		{
			Name:            "testdev",
			FirmwareVersion: "0.0.1",
			FirmwareType:    "test",
			Outdated:        false,
			IP:              net.IPv4(1, 1, 1, 1),
		},
		{
			Name:            "testdev2",
			FirmwareVersion: "0.0.2",
			FirmwareType:    "test2",
			Outdated:        true,
			IP:              net.IPv4(1, 1, 1, 2),
		},
	}

	tab := renderDeviceTable(devices)
	assert.Equal(t, "1.1.1.1 testdev  0.0.1 test          \n1.1.1.2 testdev2 0.0.2 test2 outdated", tab)
}

func TestMain(m *testing.M) {
	exitVal := m.Run()

	os.Exit(exitVal)
}
func Test_Main(t *testing.T) {
	main()
}
