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
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/viper"
	"github.com/tcnksm/go-latest"
	"github.com/tidwall/gjson"
)

// default definition for latest, to get the current version of Tasmota from GitHub
var versionData = &latest.GithubTag{
	Owner:             "arendst",
	Repository:        "tasmota",
	FixVersionStrFunc: latest.DeleteFrontV(),
}

// tasmoDevice holds basic information about a found device
type tasmoDevice struct {
	Name            string
	FirmwareVersion string
	FirmwareType    string
	Outdated        bool
	IP              net.IP
}

// ip2int converts a given IP of type net.IP to an integer.
func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

// getPasswordQuery checks if a login password was given and returns the needed URL query part
func getPasswordQuery(password string) string {
	auth := ""
	if password != "" {
		auth = "user=admin&password=" + password + "&"
	}
	return auth
}

// set up the progress bar for the scan
func initProgressBar() progress.Writer {
	pw := progress.NewWriter()
	pw.SetStyle(progress.StyleBlocks)
	pw.Style().Options.PercentFormat = "%4.1f%%"
	pw.Style().Options.TimeInProgressPrecision = time.Second
	pw.Style().Options.TimeDonePrecision = time.Second
	pw.SetOutputWriter(log.Writer())
	pw.SetAutoStop(true)
	return pw
}

// scanNetwork is the central scan function of tasmogo. It walks through the address space specified by the given CIDR and makes requests to the IPs.
func scanNetwork() []tasmoDevice {
	// convert string to IPNet struct
	_, ipv4Net, err := net.ParseCIDR(viper.GetString("cidr"))
	if err != nil {
		log.Fatal(err)
	}
	// convert IPNet struct mask and address to uint32
	// network is BigEndian
	mask := binary.BigEndian.Uint32(ipv4Net.Mask)
	// find the first address
	start := binary.BigEndian.Uint32(ipv4Net.IP)
	// find the final address
	finish := (start & mask) | (mask ^ 0xffffffff)
	// show a message and a nice progress bar.
	log.Println("Starting scan of " + strconv.Itoa(int(finish-start)) + " ip addresses (" + ipv4Net.String() + ")")

	// create a progress bar and a tracker for it to follow the progress
	pb := initProgressBar()
	tracker := progress.Tracker{Total: int64(finish - start)}
	pb.AppendTracker(&tracker)

	// The network scan is higly parallelized. So we need a wait group for the goroutines.
	var wg sync.WaitGroup
	// Writing to a slice like foundDevices with multiple goroutines results in a race condition. A mutex fixes this
	var (
		mu           = &sync.Mutex{}
		foundDevices = make([]tasmoDevice, 0)
	)
	// loop through addresses as uint32
	for i := start; i <= finish; i++ {
		wg.Add(1)
		go func(i uint32) {
			defer wg.Done()
			ip := make(net.IP, 4)
			// convert the int back to net.IP
			binary.BigEndian.PutUint32(ip, i)
			// get the device data
			device, err := getDeviceData(ip)
			if err == nil {
				// lock the mutex before writing the slice of foundDevices
				mu.Lock()
				// write and unlock
				foundDevices = append(foundDevices, device)
				mu.Unlock()
			}
			// increment the tracker progress
			tracker.Increment(1)
			// forcibly update the progressbar
			pb.Render()
		}(i)
	}
	wg.Wait()
	tracker.MarkAsDone()
	return foundDevices
}

func buildDeviceURL(hostname string, password string) string {
	auth := getPasswordQuery(password)
	return "http://" + hostname + "/cm?" + auth + "cmnd=Status%200"
}

func parseFirmwareVersion(v string) (string, string, error) {
	re, _ := regexp.Compile(`(.*)\((.*)\)`)
	res := re.FindAllStringSubmatch(v, 1)
	if len(res) != 1 {
		return "", "", errors.New("Regex parser failed\n" + v)
	}
	return res[0][1], res[0][2], nil
}

// getDeviceData loads the data from a given device ip
func getDeviceData(ip net.IP) (tasmoDevice, error) {
	var device tasmoDevice
	password := viper.GetString("password")
	// build the URL for our device request
	data, _ := getURL(buildDeviceURL(ip.String(), password))

	// Extract the firmware version
	fw := gjson.Get(data, "StatusFWR.Version").String()
	version, variant, err := parseFirmwareVersion(fw)
	if err != nil {
		return device, errors.New("Incompatible device")
	}
	// Extract the split version and type
	device.IP = ip
	device.FirmwareVersion = version
	device.FirmwareType = variant
	device.Name = gjson.Get(data, "Status.DeviceName").String()
	return device, nil
}

// getURL is a simple helper function to execute a HTTP GET request
func getURL(url string) (string, error) {
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

// getCurrentTasmotaVersion loads the current version of tasmota with help of latest
func getCurrentTasmotaVersion(v *latest.GithubTag) *version.Version {
	res, err := latest.Check(v, "0.1.0")

	if err != nil {
		log.Fatal("FATAL: Getting current Tasmota version failed.\n" + err.Error())
	}
	currentVersion, err := version.NewVersion(res.Current)
	return currentVersion
}

// checkDeviceVersion compares two version strings to evaluate if an update is needed.
func checkDeviceVersion(v *version.Version, device tasmoDevice) (tasmoDevice, error) {
	deviceVersion, _ := version.NewVersion(device.FirmwareVersion)
	if deviceVersion == nil {
		return device, errors.New("Version could not be determined")
	}
	if deviceVersion.LessThan(v) {
		device.Outdated = true
	}
	return device, nil
}

// renderDeviceTable generates a table of all found devices and their status.
func renderDeviceTable(devices []tasmoDevice) string {
	// create a table output
	t := table.NewWriter()
	// set a custom style
	t.SetStyle(table.Style{
		Name: "myNewStyle",
		Box: table.BoxStyle{
			BottomLeft:       "",
			BottomRight:      "",
			BottomSeparator:  "",
			Left:             "",
			LeftSeparator:    "",
			MiddleHorizontal: " ",
			MiddleSeparator:  "  ",
			MiddleVertical:   " ",
			PaddingLeft:      "",
			PaddingRight:     "",
			Right:            "",
			RightSeparator:   "",
			TopLeft:          "",
			TopRight:         "",
			TopSeparator:     "",
		},
		Options: table.Options{
			DrawBorder:      false,
			SeparateColumns: true,
			SeparateFooter:  false,
			SeparateHeader:  false,
			SeparateRows:    false,
		},
	})
	// walk through device list
	for _, device := range devices {
		// modify output to show "outdated" only if the device needs an update
		outdated := ""
		if device.Outdated {
			outdated = "outdated"
		}
		//append the data as a row to the table
		t.AppendRow([]interface{}{device.IP.String(), device.Name, device.FirmwareVersion, device.FirmwareType, outdated})
	}
	// print the table
	log.Println("Scan results:")
	return t.Render()
}

// updateDevices sets the OTA url of the devices and triggers an OTA update
func updateDevices(devices []tasmoDevice) {
	otaBaseURL := viper.GetString("otaurl")
	password := viper.GetString("password")
	auth := getPasswordQuery(password)

	// append tasmota to the url as files should be in the scheme "tasmota-sensors.bin"
	otaBaseURL = otaBaseURL + "tasmota"
	for _, device := range devices {
		if device.Outdated == true {
			var otaURL string
			// select filename for the default build and special variants
			if device.FirmwareType == "tasmota" {
				otaURL = otaBaseURL + ".bin"
			} else {
				otaURL = otaBaseURL + "-" + device.FirmwareType + ".bin"
			}
			log.Println("Updating " + device.Name + " (" + device.IP.String() + ") from URL: " + otaURL)
			// set the ota url
			url := "http://" + device.IP.String() + "/cm?" + auth + "cmnd=OtaUrl%20" + otaURL
			getURL(url)
			// trigger an ota upgrade
			url = "http://" + device.IP.String() + "/cm?" + auth + "cmnd=Upgrade%201"
			getURL(url)
		}
	}
}

// scanAndUpdate searches the given IP range for tasmota devices and triggers an update if enabled
func scanAndUpdate() {
	currentVersion := getCurrentTasmotaVersion(versionData)
	knownDevices := scanNetwork()

	// sort the devices by their IP address because of the parallelized run of the scan they come in a random manner
	sort.Slice(knownDevices, func(i, j int) bool {
		return ip2int(knownDevices[i].IP) < ip2int(knownDevices[j].IP)
	})

	// check if the devices need an update
	for i, device := range knownDevices {
		dev, err := checkDeviceVersion(currentVersion, device)
		if err != nil {
			continue
		}
		knownDevices[i] = dev
	}

	// show all devices
	log.Println(renderDeviceTable(knownDevices))

	// if we're supposed to du updates, do them
	if viper.GetBool("doupdates") {
		updateDevices(knownDevices)
	} else {
		log.Println("Not updating any devices. Set TASMOGO_DOUPDATES to 'true' enable automatic updates.")
	}

}

func main() {
	// load configuration data
	viper.SetConfigName("tasmogo")
	viper.AutomaticEnv()
	viper.SetEnvPrefix("tasmogo")
	viper.SetDefault("daemon", false)
	viper.SetDefault("doupdates", false)
	viper.SetDefault("otaurl", "http://ota.tasmota.com/tasmota/release/")
	viper.SetDefault("password", "")
	viper.SetDefault("cidr", "192.168.0.0/24")

	// tasmogo will run every 24h if TASMOGO_DAEMON is true.
	if viper.GetBool("daemon") {
		// do an initial scan
		scanAndUpdate()
		nextScanTime := time.Now().Local().Add(time.Hour * time.Duration(24))
		log.Println("Next scan at: " + nextScanTime.String())
		// gracefully die if requested
		var gracefulStop = make(chan os.Signal)
		signal.Notify(gracefulStop, syscall.SIGTERM)
		signal.Notify(gracefulStop, syscall.SIGINT)
		go func() {
			// gracefully die if requested
			sig := <-gracefulStop
			fmt.Println()
			fmt.Printf("caught sig: %+v", sig)
			os.Exit(0)
		}()
		// do scans every 24h and sleep inbetween
		for {
			time.Sleep(24 * time.Hour)
			scanAndUpdate()
			nextScanTime := time.Now().Local().Add(time.Hour * time.Duration(24))
			log.Println("Next scan at: " + nextScanTime.String())
		}
	} else {
		// tasmogo will run just once if TASMOGO_DAEMON is false.
		scanAndUpdate()
	}
}
