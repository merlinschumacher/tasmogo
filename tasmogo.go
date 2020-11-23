package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/robfig/cron/v3"
	"github.com/tcnksm/go-latest"
	"github.com/tidwall/gjson"
)

var versionData = &latest.GithubTag{
	Owner:             "arendst",
	Repository:        "tasmota",
	FixVersionStrFunc: latest.DeleteFrontV(),
}

type tasmoDevice struct {
	Name            string
	FirmwareVersion string
	FirmwareType    string
	Outdated        bool
	Ip              net.IP
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func getPassword() string {

	password := getEnv("TASMOGO_PASSWORD", "")
	auth := ""
	if password != "" {
		auth = "user=admin&password=" + password + "&"
	}
	return auth
}

func scanNetwork() []tasmoDevice {
	// convert string to IPNet struct
	_, ipv4Net, err := net.ParseCIDR(getEnv("TASMOGO_CIDR", "192.168.0.0/24"))
	if err != nil {
		log.Fatal(err)
	}
	var (
		mu           = &sync.Mutex{}
		foundDevices = make([]tasmoDevice, 0)
	)
	// convert IPNet struct mask and address to uint32
	// network is BigEndian
	mask := binary.BigEndian.Uint32(ipv4Net.Mask)
	start := binary.BigEndian.Uint32(ipv4Net.IP)

	// find the final address
	finish := (start & mask) | (mask ^ 0xffffffff)
	var wg sync.WaitGroup
	auth := getPassword()
	// loop through addresses as uint32
	for i := start; i <= finish; i++ {
		wg.Add(1)
		// convert back to net.IP
		go func(i uint32) {
			defer wg.Done()
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, i)
			url := "http://" + ip.String() + "/cm?" + auth + "cmnd=Status%200"
			data, _ := getUrl(url)
			device, err := getDeviceFirmware(data)
			if err == nil {
				device.Ip = ip
				mu.Lock()
				foundDevices = append(foundDevices, device)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return foundDevices

}

func getDeviceFirmware(data string) (tasmoDevice, error) {
	var device tasmoDevice
	FirmwareString := gjson.Get(data, "StatusFWR.Version")
	re, err := regexp.Compile(`(\d*\.\d*\.\d*)\((.*)\)`)
	if err != nil {
		return device, errors.New("JSON parser failed")
	}
	res := re.FindAllStringSubmatch(FirmwareString.String(), 1)
	if len(res) != 1 {
		return device, errors.New("JSON parser failed\n" + data)
	}
	device.FirmwareVersion = res[0][1]
	device.FirmwareType = res[0][2]
	device.Name = gjson.Get(data, "Status.DeviceName").String()
	return device, nil
}

func getUrl(url string) (string, error) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", url, nil)

	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("JSON download failed")
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err.Error())
	}
	return string(body), nil
}

func getCurrentTasmotaVersion() *version.Version {
	res, err := latest.Check(versionData, "0.1.0")

	if err != nil {
		log.Fatal("FATAL: Getting current Tasmota version failed.\n" + err.Error())
	}
	currentVersion, err := version.NewVersion(res.Current)
	return currentVersion
}

func checkDeviceVersion(v *version.Version, device tasmoDevice) tasmoDevice {
	deviceVersion, err := version.NewVersion(device.FirmwareVersion)
	if err != nil {
		log.Println("ERROR: Getting Tasmota version from device failed.\n" + err.Error() + "\n" + device.Ip.String())
	}
	if deviceVersion.LessThan(v) {
		device.Outdated = true
	}
	return device
}

func printDeviceTable(devices []tasmoDevice) {
	t := table.NewWriter()
	t.SetStyle(table.StyleColoredGreenWhiteOnBlack)
	t.AppendHeader(table.Row{"IP", "Name", "Version", "Type", "Outdated"})
	outdatedTransformer := text.Transformer(func(val interface{}) string {
		s := fmt.Sprintf("%v", val)
		if strings.Contains(s, "true") {
			return text.Colors{text.FgRed}.Sprint(val)
		}
		return text.Colors{text.FgGreen}.Sprint(val)
	})
	t.SetColumnConfigs([]table.ColumnConfig{{
		Name:        "Outdated",
		Transformer: outdatedTransformer,
	}})
	for _, device := range devices {
		t.AppendRow([]interface{}{device.Ip.String(), device.Name, device.FirmwareVersion, device.FirmwareType, device.Outdated})
	}

	fmt.Println(t.Render())
}

func updateDevices(devices []tasmoDevice) {
	var otaBaseUrl = getEnv("TASMOGO_OTAURL", "http://ota.tasmota.com/tasmota/release/")
	password := getEnv("TASMOGO_PASSWORD", "")
	auth := ""
	if password != "" {
		auth = "user=admin&password=" + password + "&"
	}
	otaBaseUrl = otaBaseUrl + "tasmota"
	for _, device := range devices {
		if device.Outdated == true {
			var otaUrl string
			if device.FirmwareType == "tasmota" {
				otaUrl = otaBaseUrl + ".bin"

			} else {
				otaUrl = otaBaseUrl + "-" + device.FirmwareType + ".bin"
			}
			fmt.Println("Updating " + device.Name + " (" + device.Ip.String() + ") from URL: " + otaUrl)
			url := "http://" + device.Ip.String() + "/cm?" + auth + "cmnd=OtaUrl%20" + otaUrl
			getUrl(url)
			url = "http://" + device.Ip.String() + "/cm?" + auth + "cmnd=Upgrade%201"
			getUrl(url)
		}
	}

}

func scanAndUpdate() {
	currentVersion := getCurrentTasmotaVersion()
	knownDevices := scanNetwork()
	sort.Slice(knownDevices, func(i, j int) bool {

		return ip2int(knownDevices[i].Ip) < ip2int(knownDevices[j].Ip)
	})
	for i, device := range knownDevices {
		knownDevices[i] = checkDeviceVersion(currentVersion, device)
	}
	printDeviceTable(knownDevices)
	doUpdate := getEnv("TASMOGO_DOUPDATES", "false")
	if doUpdate == "true" {
		updateDevices(knownDevices)
	} else {
		fmt.Println("Not updating any devices. Set TASMOGO_DOUPDATES to 'true' enable automatic updates.")
	}

}

func main() {
	if getEnv("TASMOGO_DAEMON", "false") == "true" {

		c := cron.New()
		c.AddFunc("@hourly", func() { scanAndUpdate() })
		c.Start()
		scanAndUpdate()
		var gracefulStop = make(chan os.Signal)
		signal.Notify(gracefulStop, syscall.SIGTERM)
		signal.Notify(gracefulStop, syscall.SIGINT)
		go func() {
			sig := <-gracefulStop
			c.Stop()
			fmt.Println()
			fmt.Printf("caught sig: %+v", sig)
			os.Exit(0)
		}()
		for {
		}
	} else {
		scanAndUpdate()
	}
}
