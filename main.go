package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const hc = "headsetcontrol"

type chargePoint struct {
	t     time.Time
	level int
}

const chargeHistoryMax = 60 // 60 samples * 5s = 5 min window

// ---------- JSON model ----------------------------------------------------

type hcDevice struct {
	Device       string   `json:"device"`
	Vendor       string   `json:"vendor"`
	Product      string   `json:"product"`
	IDVendor     string   `json:"id_vendor"`
	IDProduct    string   `json:"id_product"`
	Capabilities []string `json:"capabilities_str"`
	Battery      struct {
		Status string `json:"status"`
		Level  int    `json:"level"`
	} `json:"battery"`
	EqPresets map[string][]float64 `json:"equalizer_presets"`
}

type hcOutput struct {
	Devices []hcDevice `json:"devices"`
}

func fetchInfo() (*hcOutput, error) {
	out, err := exec.Command(hc, "-o", "json").Output()
	if err != nil {
		return nil, err
	}
	var o hcOutput
	if err := json.Unmarshal(out, &o); err != nil {
		return nil, err
	}
	if len(o.Devices) == 0 {
		return nil, fmt.Errorf("no devices found")
	}
	return &o, nil
}

func runCmd(args ...string) error {
	out, err := exec.Command(hc, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runCmdForDevice(vid, pid string, args ...string) error {
	full := append([]string{"-d", vid + ":" + pid}, args...)
	return runCmd(full...)
}

func hasCap(caps []string, name string) bool {
	for _, c := range caps {
		if c == name {
			return true
		}
	}
	return false
}

// ---------- System --------------------------------------------------------

func setVolume(p int) error {
	return exec.Command("pactl", "set-sink-volume", "@DEFAULT_SINK@",
		fmt.Sprintf("%d%%", p)).Run()
}
func toggleMute() error {
	return exec.Command("pactl", "set-sink-mute", "@DEFAULT_SINK@", "toggle").Run()
}
func toggleMic() error {
	return exec.Command("pactl", "set-source-mute", "@DEFAULT_SOURCE@", "toggle").Run()
}

func notify(title, body string) {
	_ = exec.Command("notify-send", "-u", "critical", "-a", "Headset Control",
		title, body).Run()
}

func fmtDuration(minutes float64) string {
	if minutes < 1 {
		return "<1 min"
	}
	if minutes < 60 {
		return fmt.Sprintf("%.0f min", minutes)
	}
	h := int(minutes) / 60
	m := int(minutes) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// ---------- Dark theme ----------------------------------------------------

type darkTheme struct{}

var (
	bgColor     = color.NRGBA{0x12, 0x14, 0x18, 0xff}
	cardColor   = color.NRGBA{0x1c, 0x1f, 0x26, 0xff}
	accentColor = color.NRGBA{0x3d, 0xd6, 0x8c, 0xff}
	mutedColor  = color.NRGBA{0x8a, 0x90, 0x9e, 0xff}
	textColor   = color.NRGBA{0xe6, 0xe8, 0xee, 0xff}
	overlayBg   = color.NRGBA{0x22, 0x26, 0x2e, 0xff}
)

func (darkTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return bgColor
	case theme.ColorNameForeground, theme.ColorNameForegroundOnPrimary:
		return textColor
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return accentColor
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return cardColor
	case theme.ColorNameDisabled:
		return mutedColor
	case theme.ColorNameSeparator:
		return color.NRGBA{0x2a, 0x2e, 0x38, 0xff}
	case theme.ColorNameHover:
		return color.NRGBA{0x24, 0x28, 0x32, 0xff}
	case theme.ColorNamePressed:
		return color.NRGBA{0x2c, 0x31, 0x3c, 0xff}
	case theme.ColorNameSelection:
		return color.NRGBA{0x3d, 0xd6, 0x8c, 0x22}
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return overlayBg
	case theme.ColorNamePlaceHolder:
		return mutedColor
	}
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}
func (darkTheme) Font(s fyne.TextStyle) fyne.Resource    { return theme.DefaultTheme().Font(s) }
func (darkTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (darkTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameText:
		return 13
	}
	return theme.DefaultTheme().Size(n)
}

// ---------- Card helper ---------------------------------------------------

func card(title string, body fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cardColor)
	bg.CornerRadius = 10
	t := canvas.NewText(strings.ToUpper(title), mutedColor)
	t.TextSize = 11
	t.TextStyle = fyne.TextStyle{Bold: true}
	content := container.NewPadded(container.NewVBox(t, body))
	return container.NewStack(bg, content)
}

// ---------- App -----------------------------------------------------------

func main() {
	a := app.New()
	a.Settings().SetTheme(darkTheme{})

	w := a.NewWindow("Headset Control")
	w.Resize(fyne.NewSize(380, 640))

	// Header
	devName := canvas.NewText("Connecting…", textColor)
	devName.TextSize = 16
	devName.TextStyle = fyne.TextStyle{Bold: true}

	batPct := canvas.NewText("--%", mutedColor)
	batPct.TextSize = 26
	batPct.TextStyle = fyne.TextStyle{Bold: true}
	batStatus := canvas.NewText("", mutedColor)
	batStatus.TextSize = 11

	// Device selector (only shown when 2+ devices)
	deviceSel := widget.NewSelect([]string{}, nil)
	deviceSelWrap := container.NewVBox(deviceSel)
	deviceSelWrap.Hide()

	header := card("Device", container.NewVBox(
		deviceSelWrap,
		container.NewBorder(nil, nil,
			container.NewVBox(devName, batStatus),
			batPct,
		),
	))

	// Sections
	sections := container.NewVBox()

	// System audio
	volSlider := widget.NewSlider(0, 100)
	volSlider.Value = 50
	volSlider.OnChangeEnded = func(v float64) { _ = setVolume(int(v)) }
	muteBtn := widget.NewButton("Mute Output", func() { _ = toggleMute() })
	micBtn := widget.NewButton("Mute Mic", func() { _ = toggleMic() })
	sysCard := card("System Audio", container.NewVBox(
		volSlider,
		container.NewGridWithColumns(2, muteBtn, micBtn),
	))

	// State
	var (
		currentIdx     = 0
		lastDevices    []hcDevice
		builtForKey    string
		lowNotifiedFor = map[string]bool{}
		chargeHistory  = map[string][]chargePoint{}
	)

	rebuildSections := func(d hcDevice) {
		sections.Objects = nil

		if hasCap(d.Capabilities, "sidetone") {
			sideVal := canvas.NewText("0", textColor)
			sideVal.TextSize = 13
			s := widget.NewSlider(0, 128)
			s.Step = 1
			s.OnChanged = func(v float64) { sideVal.Text = strconv.Itoa(int(v)); sideVal.Refresh() }
			s.OnChangeEnded = func(v float64) {
				_ = runCmdForDevice(d.IDVendor, d.IDProduct, "-s", strconv.Itoa(int(v)))
			}
			sections.Add(card("Sidetone (mic feedback)",
				container.NewBorder(nil, nil, nil, sideVal, s)))
		}

		if hasCap(d.Capabilities, "inactive time") {
			entry := widget.NewEntry()
			entry.SetPlaceHolder("0–90 (0 = never)")
			btn := widget.NewButton("Apply", func() {
				n, err := strconv.Atoi(strings.TrimSpace(entry.Text))
				if err != nil || n < 0 || n > 90 {
					dialog.ShowError(fmt.Errorf("enter 0–90"), w)
					return
				}
				if err := runCmdForDevice(d.IDVendor, d.IDProduct, "-i", strconv.Itoa(n)); err != nil {
					dialog.ShowError(err, w)
				}
			})
			sections.Add(card("Auto-off (minutes)",
				container.NewBorder(nil, nil, nil, btn, entry)))
		}

		if hasCap(d.Capabilities, "equalizer") && len(d.EqPresets) > 0 {
			eq := container.NewVBox(buildEQ(d)...)
			sections.Add(card("Equalizer", eq))
		} else if hasCap(d.Capabilities, "equalizer preset") {
			sel := widget.NewSelect([]string{"0", "1", "2", "3", "4"}, func(s string) {
				_ = runCmdForDevice(d.IDVendor, d.IDProduct, "-p", s)
			})
			sections.Add(card("Equalizer preset", sel))
		}

		sections.Add(sysCard)
		sections.Refresh()
	}

	updateHeader := func(d hcDevice) {
		devName.Text = d.Device
		devName.Refresh()

		if !hasCap(d.Capabilities, "battery") {
			batPct.Text = ""
			batPct.Refresh()
			batStatus.Text = ""
			batStatus.Refresh()
			return
		}

		key := d.IDVendor + ":" + d.IDProduct
		charging := strings.Contains(d.Battery.Status, "CHARGING")
		level := d.Battery.Level

		batPct.Text = fmt.Sprintf("%d%%", level)
		switch {
		case charging:
			batPct.Color = accentColor
		case level < 20:
			batPct.Color = color.NRGBA{0xff, 0x6b, 0x6b, 0xff}
		case level < 40:
			batPct.Color = color.NRGBA{0xff, 0xc0, 0x4d, 0xff}
		default:
			batPct.Color = accentColor
		}
		batPct.Refresh()

		// Charging history & ETA
		if charging {
			hist := chargeHistory[key]
			if len(hist) > 0 && level < hist[len(hist)-1].level {
				hist = nil
			}
			hist = append(hist, chargePoint{time.Now(), level})
			if len(hist) > chargeHistoryMax {
				hist = hist[len(hist)-chargeHistoryMax:]
			}
			chargeHistory[key] = hist

			statusLine := "Charging"
			if level >= 100 {
				statusLine = "Fully charged"
			} else if len(hist) >= 2 {
				first := hist[0]
				last := hist[len(hist)-1]
				dt := last.t.Sub(first.t).Minutes()
				dl := last.level - first.level
				if dt > 0.3 && dl > 0 {
					rate := float64(dl) / dt
					remaining := float64(100-level) / rate
					statusLine = fmt.Sprintf("Charging · %.1f%%/min · full in %s",
						rate, fmtDuration(remaining))
				} else {
					statusLine = "Charging · measuring…"
				}
			} else {
				statusLine = "Charging · measuring…"
			}
			batStatus.Text = statusLine
		} else {
			delete(chargeHistory, key)
			batStatus.Text = strings.TrimPrefix(d.Battery.Status, "BATTERY_")
		}
		batStatus.Refresh()

		// Low battery notification (only when not charging)
		if level <= 10 && !charging {
			if !lowNotifiedFor[key] {
				notify("Headset battery low",
					fmt.Sprintf("%s at %d%%", d.Device, level))
				lowNotifiedFor[key] = true
			}
		} else if level > 15 || charging {
			lowNotifiedFor[key] = false
		}
	}

	deviceSel.OnChanged = func(s string) {
		for i, d := range lastDevices {
			label := fmt.Sprintf("%s (%s)", d.Device, d.IDProduct)
			if label == s {
				currentIdx = i
				key := d.IDVendor + ":" + d.IDProduct
				if builtForKey != key {
					rebuildSections(d)
					builtForKey = key
				}
				updateHeader(d)
				return
			}
		}
	}

	refresh := func() {
		info, err := fetchInfo()
		if err != nil {
			devName.Text = "No headset detected"
			devName.Refresh()
			batPct.Text = "--%"
			batPct.Color = mutedColor
			batPct.Refresh()
			batStatus.Text = err.Error()
			batStatus.Refresh()
			deviceSelWrap.Hide()
			return
		}
		lastDevices = info.Devices

		if len(info.Devices) > 1 {
			opts := make([]string, len(info.Devices))
			for i, d := range info.Devices {
				opts[i] = fmt.Sprintf("%s (%s)", d.Device, d.IDProduct)
			}
			deviceSel.Options = opts
			if currentIdx >= len(opts) {
				currentIdx = 0
			}
			deviceSel.SetSelected(opts[currentIdx])
			deviceSelWrap.Show()
		} else {
			deviceSelWrap.Hide()
			currentIdx = 0
		}

		d := info.Devices[currentIdx]
		key := d.IDVendor + ":" + d.IDProduct
		if builtForKey != key {
			rebuildSections(d)
			builtForKey = key
		}
		updateHeader(d)
	}

	root := container.NewBorder(
		container.NewPadded(header),
		nil,
		nil, nil,
		container.NewPadded(sections),
	)
	w.SetContent(root)

	go func() {
		refresh()
		for range time.Tick(5 * time.Second) {
			fyne.Do(refresh)
		}
	}()

	w.ShowAndRun()
}

// ---------- EQ widget -----------------------------------------------------

func buildEQ(d hcDevice) []fyne.CanvasObject {
	presets := d.EqPresets
	names := make([]string, 0, len(presets))
	for k := range presets {
		names = append(names, k)
	}
	sort.Strings(names)

	first := "flat"
	if _, ok := presets[first]; !ok {
		first = names[0]
	}
	bands := len(presets[first])

	freqLabels := []string{"60Hz", "250Hz", "1KHz", "4KHz", "12KHz"}
	if bands != 5 {
		freqLabels = make([]string, bands)
		for i := range freqLabels {
			freqLabels[i] = fmt.Sprintf("B%d", i+1)
		}
	}

	sliders := make([]*widget.Slider, bands)
	valueLabels := make([]*canvas.Text, bands)

	apply := func() {
		vals := make([]string, bands)
		for i, s := range sliders {
			vals[i] = strconv.FormatFloat(s.Value, 'f', 1, 64)
		}
		_ = runCmdForDevice(d.IDVendor, d.IDProduct, "-e", strings.Join(vals, ","))
	}

	for i := 0; i < bands; i++ {
		val := canvas.NewText("0.0", textColor)
		val.TextSize = 11
		val.Alignment = fyne.TextAlignCenter
		valueLabels[i] = val

		s := widget.NewSlider(-12, 12)
		s.Step = 0.5
		s.Orientation = widget.Vertical
		idx := i
		s.OnChanged = func(v float64) {
			valueLabels[idx].Text = strconv.FormatFloat(v, 'f', 1, 64)
			valueLabels[idx].Refresh()
		}
		s.OnChangeEnded = func(_ float64) { apply() }
		sliders[i] = s
	}

	presetSel := widget.NewSelect(names, func(name string) {
		vals := presets[name]
		for i, v := range vals {
			if i < len(sliders) {
				sliders[i].SetValue(v)
				valueLabels[i].Text = strconv.FormatFloat(v, 'f', 1, 64)
				valueLabels[i].Refresh()
			}
		}
		apply()
	})
	presetSel.SetSelected(first)

	reset := widget.NewButton("Reset", func() {
		for i, s := range sliders {
			s.SetValue(0)
			valueLabels[i].Text = "0.0"
			valueLabels[i].Refresh()
		}
		apply()
	})

	cols := make([]fyne.CanvasObject, bands)
	for i := 0; i < bands; i++ {
		freq := canvas.NewText(freqLabels[i], mutedColor)
		freq.TextSize = 11
		freq.Alignment = fyne.TextAlignCenter
		sliderBox := container.NewGridWrap(fyne.NewSize(40, 140), sliders[i])
		cols[i] = container.NewVBox(valueLabels[i], sliderBox, freq)
	}

	grid := container.NewGridWithColumns(bands, cols...)
	topRow := container.NewBorder(nil, nil,
		widget.NewLabel("Preset:"), reset, presetSel)

	return []fyne.CanvasObject{topRow, grid}
}
