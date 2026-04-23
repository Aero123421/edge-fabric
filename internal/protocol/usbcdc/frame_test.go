package usbcdc

import "testing"

func TestRoundTrip(t *testing.T) {
	frame, err := EncodeFrame(3, []byte("hello-fabric"))
	if err != nil {
		t.Fatal(err)
	}
	frameType, payload, err := DecodeFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if frameType != 3 {
		t.Fatalf("unexpected frame type: %d", frameType)
	}
	if string(payload) != "hello-fabric" {
		t.Fatalf("unexpected payload: %s", string(payload))
	}
}

func TestRejectsTrailingBytes(t *testing.T) {
	frame, err := EncodeFrame(3, []byte("hello-fabric"))
	if err != nil {
		t.Fatal(err)
	}
	frame = append(frame, byte('x'))
	if _, _, err := DecodeFrame(frame); err == nil {
		t.Fatal("expected trailing byte error")
	}
}

func TestRejectsHeaderTamper(t *testing.T) {
	frame, err := EncodeFrame(3, []byte("hello-fabric"))
	if err != nil {
		t.Fatal(err)
	}
	frame[3] = 2
	if _, _, err := DecodeFrame(frame); err == nil {
		t.Fatal("expected header tamper error")
	}
}
