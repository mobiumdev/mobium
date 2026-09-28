package device

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"net"
	"os"
	"time"

	"github.com/mobiumdev/mobium/internal/plist"
)

// A real iPhone's Web Inspector is reached the way Appium and
// ios_webkit_debug_proxy reach it, and every step was measured on an iPhone
// 15 Plus on iOS 26.6.2 before any of this was written:
//
//  1. usbmuxd, a Unix socket on every Mac, lists the phone and hands over the
//     pairing record the Mac made when someone tapped "Trust".
//  2. Through it, lockdown (device port 62078) opens a session, upgraded to
//     TLS with that record's host certificate.
//  3. Lockdown starts com.apple.webinspector and says which port it is on,
//     and that the service wants TLS too.
//  4. That port, through usbmuxd again and under TLS, speaks exactly the
//     protocol a simulator's webinspectord socket does — so everything in
//     internal/webview is used unchanged.
//
// The iOS 17+ route was tried first and is not usable from here. The CoreDevice
// tunnel does carry a com.apple.webinspector.shim.remote service, and it
// answers the same protocol after a check-in, but its port is assigned per
// connection and only the tunnel's service discovery knows it — and neither
// `remotectl netcat` (refused without an entitlement) nor any plain port on
// the tunnel would give it up. Finding it by trying ports means checking in
// with every service on the phone, one of which answered `ShowDialog: true`.
//
// Nothing here needs root: /var/run/usbmuxd is world-writable, and it serves
// pairing records to any local user — which is also why this works only for a
// phone this Mac has been trusted by.

// usbmuxdSocket is where macOS's usbmuxd listens.
func usbmuxdSocket() string {
	if p := os.Getenv("MOBIUM_USBMUXD_SOCKET"); p != "" {
		return p
	}
	return "/var/run/usbmuxd"
}

// lockdownPort is lockdown's fixed port on every iOS device.
const lockdownPort = 62078

// muxTimeout bounds each usbmuxd and lockdown exchange.
const muxTimeout = 10 * time.Second

// muxRequest sends one plist request to usbmuxd and returns the reply along
// with the connection, which after a successful Connect has become a byte
// stream to the device port.
func muxRequest(ctx context.Context, msg map[string]any) (net.Conn, map[string]any, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", usbmuxdSocket())
	if err != nil {
		return nil, nil, fmt.Errorf("could not reach usbmuxd at %s: %w", usbmuxdSocket(), err)
	}
	_ = conn.SetDeadline(time.Now().Add(muxTimeout))
	msg["ClientVersionString"] = "mobium"
	msg["ProgName"] = "mobium"
	body, err := plist.Marshal(msg)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	// Header: total length, protocol version 1, message type 8 (plist), tag.
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], uint32(16+len(body)))
	binary.LittleEndian.PutUint32(hdr[4:], 1)
	binary.LittleEndian.PutUint32(hdr[8:], 8)
	binary.LittleEndian.PutUint32(hdr[12:], 1)
	if _, err := conn.Write(append(hdr, body...)); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("usbmuxd: %w", err)
	}
	if _, err := io.ReadFull(conn, hdr); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("usbmuxd: %w", err)
	}
	n := binary.LittleEndian.Uint32(hdr[0:])
	if n < 16 || n > 1<<20 {
		conn.Close()
		return nil, nil, mobiumerr.New(mobiumerr.DeviceServer, "usbmuxd: implausible reply length %d", n)
	}
	reply := make([]byte, n-16)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("usbmuxd: %w", err)
	}
	v, err := plist.Unmarshal(reply)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("usbmuxd: %w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		conn.Close()
		return nil, nil, mobiumerr.New(mobiumerr.DeviceServer, "usbmuxd: reply is not a dictionary")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, m, nil
}

func muxAsk(ctx context.Context, msg map[string]any) (map[string]any, error) {
	conn, m, err := muxRequest(ctx, msg)
	if err != nil {
		return nil, err
	}
	conn.Close()
	return m, nil
}

// muxDeviceID finds the phone usbmuxd knows by its UDID.
func muxDeviceID(ctx context.Context, udid string) (int64, error) {
	m, err := muxAsk(ctx, map[string]any{"MessageType": "ListDevices"})
	if err != nil {
		return 0, err
	}
	list, _ := m["DeviceList"].([]any)
	for _, d := range list {
		dm, _ := d.(map[string]any)
		props, _ := dm["Properties"].(map[string]any)
		if props["SerialNumber"] == udid {
			if id, ok := props["DeviceID"].(int64); ok {
				return id, nil
			}
		}
	}
	return 0, mobiumerr.New(mobiumerr.DeviceNotReady, "usbmuxd does not list %s — what lockdown serves (WebViews, "+
		"the device log, crash reports, the clock) needs the phone connected by cable", udid).
		WithRemedy("connect the iPhone with a cable, unlocked and trusting this Mac")
}

// muxConnect opens a stream to a port on the device.
func muxConnect(ctx context.Context, deviceID int64, port int) (net.Conn, error) {
	// usbmuxd takes the port in network byte order inside a little-endian
	// integer, which is the swap here.
	swapped := int64((port&0xff)<<8 | (port>>8)&0xff)
	conn, m, err := muxRequest(ctx, map[string]any{
		"MessageType": "Connect", "DeviceID": deviceID, "PortNumber": swapped,
	})
	if err != nil {
		return nil, err
	}
	if n, _ := m["Number"].(int64); n != 0 || m["MessageType"] != "Result" {
		conn.Close()
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "usbmuxd could not connect to device port %d (result %v)", port, m["Number"])
	}
	return conn, nil
}

// pairRecord is what lockdown needs from the Mac's pairing with the phone.
type pairRecord struct {
	hostID, systemBUID string
	cert               tls.Certificate
}

func readPairRecord(ctx context.Context, udid string) (*pairRecord, error) {
	m, err := muxAsk(ctx, map[string]any{"MessageType": "ReadPairRecord", "PairRecordID": udid})
	if err != nil {
		return nil, err
	}
	data, ok := m["PairRecordData"].([]byte)
	if !ok {
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "this Mac has no pairing record for %s — unlock the phone, "+
			"tap Trust This Computer, and try again", udid)
	}
	v, err := plist.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("pairing record: %w", err)
	}
	rec, _ := v.(map[string]any)
	certPEM, _ := rec["HostCertificate"].([]byte)
	keyPEM, _ := rec["HostPrivateKey"].([]byte)
	hostID, _ := rec["HostID"].(string)
	buid, _ := rec["SystemBUID"].(string)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || hostID == "" || buid == "" {
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "the pairing record for %s is incomplete", udid)
	}
	return &pairRecord{hostID: hostID, systemBUID: buid, cert: cert}, nil
}

// lockdownTLS wraps a device stream the way lockdown and its services expect:
// our host certificate, and no verification of theirs — the device presents a
// certificate from the pairing, not a chain anything here could check.
func lockdownTLS(conn net.Conn, rec *pairRecord) (net.Conn, error) {
	c := tls.Client(conn, &tls.Config{
		Certificates:       []tls.Certificate{rec.cert},
		InsecureSkipVerify: true, // the peer is the paired phone; see above
		MinVersion:         tls.VersionTLS12,
	})
	_ = c.SetDeadline(time.Now().Add(muxTimeout))
	if err := c.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS with the phone: %w", err)
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

// lockdownCall sends one request in lockdown's framing — four bytes of
// big-endian length, then a plist, the same framing the web inspector uses —
// and reads the answer.
func lockdownCall(conn net.Conn, req map[string]any) (map[string]any, error) {
	_ = conn.SetDeadline(time.Now().Add(muxTimeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	req["Label"] = "mobium"
	body, err := plist.Marshal(req)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(frame, uint32(len(body)))
	if _, err := conn.Write(append(frame, body...)); err != nil {
		return nil, fmt.Errorf("lockdown: %w", err)
	}
	if _, err := io.ReadFull(conn, frame[:4]); err != nil {
		return nil, fmt.Errorf("lockdown: %w", err)
	}
	n := binary.BigEndian.Uint32(frame[:4])
	if n > 1<<20 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "lockdown: implausible reply length %d", n)
	}
	reply := make([]byte, n)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return nil, fmt.Errorf("lockdown: %w", err)
	}
	v, err := plist.Unmarshal(reply)
	if err != nil {
		return nil, fmt.Errorf("lockdown: %w", err)
	}
	m, _ := v.(map[string]any)
	if e, ok := m["Error"].(string); ok {
		return m, mobiumerr.New(mobiumerr.DeviceServer, "lockdown refused %s: %s", req["Request"], e)
	}
	return m, nil
}

// WebInspectorConn opens a stream to a real iPhone's Web Inspector, ready for
// the same protocol a simulator's webinspectord socket speaks.
func WebInspectorConn(ctx context.Context, udid string) (net.Conn, error) {
	return LockdownService(ctx, udid, "com.apple.webinspector")
}

// lockdownSession connects to a paired phone's lockdown and starts a session,
// under TLS when lockdown asks for it. The caller closes the connection.
func lockdownSession(ctx context.Context, udid string) (net.Conn, int64, *pairRecord, error) {
	id, err := muxDeviceID(ctx, udid)
	if err != nil {
		return nil, 0, nil, err
	}
	rec, err := readPairRecord(ctx, udid)
	if err != nil {
		return nil, 0, nil, err
	}
	ld, err := muxConnect(ctx, id, lockdownPort)
	if err != nil {
		return nil, 0, nil, err
	}
	sess, err := lockdownCall(ld, map[string]any{
		"Request": "StartSession", "HostID": rec.hostID, "SystemBUID": rec.systemBUID,
	})
	if err != nil {
		ld.Close()
		return nil, 0, nil, err
	}
	if on, _ := sess["EnableSessionSSL"].(bool); on {
		tconn, err := lockdownTLS(ld, rec)
		if err != nil {
			ld.Close()
			return nil, 0, nil, err
		}
		return tconn, id, rec, nil
	}
	return ld, id, rec, nil
}

// LockdownValues reads values lockdown keeps about a paired phone — such as
// TimeIntervalSince1970, the phone's own clock — in one session.
func LockdownValues(ctx context.Context, udid string, keys ...string) (map[string]any, error) {
	lconn, _, _, err := lockdownSession(ctx, udid)
	if err != nil {
		return nil, err
	}
	defer lconn.Close()
	out := map[string]any{}
	for _, k := range keys {
		m, err := lockdownCall(lconn, map[string]any{"Request": "GetValue", "Key": k})
		if err != nil {
			return nil, err
		}
		v, ok := m["Value"]
		if !ok {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "lockdown has no value for %s", k)
		}
		out[k] = v
	}
	return out, nil
}

// LockdownService asks a paired phone's lockdown to start a service and
// returns a stream to it, under TLS when the service asks for it. Every
// service a phone offers over USB without a developer tunnel is reached this
// way; only the name differs.
func LockdownService(ctx context.Context, udid, service string) (net.Conn, error) {
	lconn, id, rec, err := lockdownSession(ctx, udid)
	if err != nil {
		return nil, err
	}
	defer lconn.Close()
	svc, err := lockdownCall(lconn, map[string]any{
		"Request": "StartService", "Service": service,
	})
	if err != nil {
		return nil, err
	}
	port, _ := svc["Port"].(int64)
	if port <= 0 || port > 65535 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "lockdown started %s and gave no port", service)
	}

	conn, err := muxConnect(ctx, id, int(port))
	if err != nil {
		return nil, err
	}
	if on, _ := svc["EnableServiceSSL"].(bool); on {
		tconn, err := lockdownTLS(conn, rec)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return tconn, nil
	}
	return conn, nil
}
