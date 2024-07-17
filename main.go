package main

import (
	"encoding/gob"
	"fmt"
	"net"
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/go-vgo/robotgo"
	hook "github.com/robotn/gohook"
)

type EventType int

const (
	KeyDown EventType = iota
	KeyUp
	MouseMove
	MouseDown
	MouseUp
	MouseWheel
)

type Event struct {
	Type      EventType
	Key       string
	X, Y      int
	Button    string
	Direction int
}

type SharerApp struct {
	isServer     bool
	serverAddr   string
	port         int
	conn         net.Conn
	listener     net.Listener
	encoder      *gob.Encoder
	decoder      *gob.Decoder
	window       fyne.Window
	status       *widget.Label
	startButton  *widget.Button
	stopButton   *widget.Button
	addrEntry    *widget.Entry
	portEntry    *widget.Entry
	modeSelect   *widget.Select
	screenWidth  int
	screenHeight int
	running      bool
	mutex        sync.Mutex
	ipLabel      *widget.Label
}

func newSharerApp() *SharerApp {
	a := app.New()
	w := a.NewWindow("Keyboard and Mouse Sharer")
	w.Resize(fyne.NewSize(400, 300))

	width, height := robotgo.GetScreenSize()
	sa := &SharerApp{
		window:       w,
		serverAddr:   "localhost",
		port:         12345,
		screenWidth:  width,
		screenHeight: height,
	}

	sa.createUI()
	return sa
}

func (sa *SharerApp) createUI() {
	// Initialize all UI elements first
	sa.status = widget.NewLabel("Not connected")
	sa.startButton = widget.NewButton("Start", sa.start)
	sa.stopButton = widget.NewButton("Stop", sa.stop)
	sa.stopButton.Disable()

	sa.addrEntry = widget.NewEntry()
	sa.addrEntry.SetText(sa.serverAddr)
	sa.portEntry = widget.NewEntry()
	sa.portEntry.SetText(strconv.Itoa(sa.port))

	sa.ipLabel = widget.NewLabel("")

	sa.modeSelect = widget.NewSelect([]string{"Server", "Client"}, func(mode string) {
		sa.isServer = mode == "Server"
		if sa.isServer {
			sa.addrEntry.SetText("localhost")
			sa.addrEntry.Disable()
		} else {
			sa.addrEntry.Enable()
		}
		sa.updateIPLabel()
	})
	sa.modeSelect.SetSelected("Server")

	// Now create the content layout
	content := container.NewVBox(
		widget.NewLabel("Mode:"),
		sa.modeSelect,
		widget.NewLabel("Server Address:"),
		sa.addrEntry,
		widget.NewLabel("Port:"),
		sa.portEntry,
		container.NewHBox(sa.startButton, sa.stopButton),
		widget.NewLabel("Status:"),
		sa.status,
		widget.NewLabel("Local IP:"),
		sa.ipLabel,
	)

	sa.window.SetContent(container.NewPadded(content))

	// Update IP label after everything is set up
	sa.updateIPLabel()
}

func (sa *SharerApp) updateIPLabel() {
	if sa.isServer {
		sa.ipLabel.SetText(getLocalIP())
	} else {
		sa.ipLabel.SetText("N/A (Client Mode)")
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "Unable to determine IP address"
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "No IP address found"
}

func (sa *SharerApp) start() {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	if sa.running {
		return
	}

	var err error
	sa.port, err = strconv.Atoi(sa.portEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf("invalid port number"), sa.window)
		return
	}

	sa.serverAddr = sa.addrEntry.Text

	sa.running = true
	if sa.isServer {
		go sa.runServer()
	} else {
		go sa.runClient()
	}

	sa.startButton.Disable()
	sa.stopButton.Enable()
	sa.modeSelect.Disable()

	sa.updateIPLabel()
}

func (sa *SharerApp) stop() {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	if !sa.running {
		return
	}

	sa.running = false
	if sa.conn != nil {
		sa.conn.Close()
	}
	if sa.listener != nil {
		sa.listener.Close()
	}
	hook.End()

	sa.startButton.Enable()
	sa.stopButton.Disable()
	sa.modeSelect.Enable()
	sa.status.SetText("Stopped")

	sa.updateIPLabel()
}

func (sa *SharerApp) runServer() {
	defer sa.stop()

	var err error
	sa.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", sa.port))
	if err != nil {
		sa.status.SetText(fmt.Sprintf("Error starting server: %v", err))
		return
	}
	defer sa.listener.Close()

	sa.status.SetText(fmt.Sprintf("Server is listening on :%d", sa.port))

	sa.conn, err = sa.listener.Accept()
	if err != nil {
		sa.status.SetText(fmt.Sprintf("Error accepting connection: %v", err))
		return
	}
	defer sa.conn.Close()

	sa.status.SetText(fmt.Sprintf("Client connected: %v", sa.conn.RemoteAddr()))

	sa.encoder = gob.NewEncoder(sa.conn)

	evChan := hook.Start()
	defer hook.End()

	for ev := range evChan {
		var event Event
		switch ev.Kind {
		case hook.KeyDown:
			event = Event{Type: KeyDown, Key: strconv.Itoa(int(ev.Rawcode))}
		case hook.KeyUp:
			event = Event{Type: KeyUp, Key: strconv.Itoa(int(ev.Rawcode))}
		case hook.MouseMove:
			x, y := robotgo.Location()
			event = Event{Type: MouseMove, X: x, Y: y}
		case hook.MouseDown:
			event = Event{Type: MouseDown, Button: strconv.Itoa(int(ev.Button))}
		case hook.MouseUp:
			event = Event{Type: MouseUp, Button: strconv.Itoa(int(ev.Button))}
		case hook.MouseWheel:
			event = Event{Type: MouseWheel, Direction: int(ev.Rotation)}
		default:
			continue
		}

		if event.Type == MouseMove {
			event.X = int(float64(event.X) / float64(sa.screenWidth) * 100)
			event.Y = int(float64(event.Y) / float64(sa.screenHeight) * 100)
		}

		err := sa.encoder.Encode(event)
		if err != nil {
			sa.status.SetText(fmt.Sprintf("Error sending event: %v", err))
			return
		}
	}
}

func (sa *SharerApp) runClient() {
	defer sa.stop()

	var err error
	sa.conn, err = net.Dial("tcp", fmt.Sprintf("%s:%d", sa.serverAddr, sa.port))
	if err != nil {
		sa.status.SetText(fmt.Sprintf("Error connecting to server: %v", err))
		return
	}
	defer sa.conn.Close()

	sa.status.SetText(fmt.Sprintf("Connected to server: %v", sa.conn.RemoteAddr()))

	sa.decoder = gob.NewDecoder(sa.conn)

	for sa.running {
		var event Event
		err := sa.decoder.Decode(&event)
		if err != nil {
			sa.status.SetText(fmt.Sprintf("Error receiving event: %v", err))
			return
		}

		switch event.Type {
		case KeyDown:
			robotgo.KeyDown(event.Key)
		case KeyUp:
			robotgo.KeyUp(event.Key)
		case MouseMove:
			x := int(float64(event.X) / 100 * float64(sa.screenWidth))
			y := int(float64(event.Y) / 100 * float64(sa.screenHeight))
			robotgo.Move(x, y)
		case MouseDown:
			robotgo.Click(event.Button)
		case MouseUp:
			robotgo.Toggle(event.Button, "up")
		case MouseWheel:
			robotgo.Scroll(0, event.Direction)
		}
	}
}

func main() {
	sa := newSharerApp()
	sa.window.ShowAndRun()
}
