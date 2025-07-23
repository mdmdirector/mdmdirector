package director

import (
	"testing"
)

const testBodyDeviceLock = `{
  "commands": [
    {
      "uuid": "7781b53a-6a35-4b2d-940e-fb8e57dadba5",
      "payload": "PD94bWwgdmVyc2lvbj0iMS4wIiBlbmNvZGluZz0iVVRGLTgiPz4KPCFET0NUWVBFIHBsaXN0IFBVQkxJQyAiLS8vQXBwbGUvL0RURCBQTElTVCAxLjAvL0VOIiAiaHR0cDovL3d3dy5hcHBsZS5jb20vRFREcy9Qcm9wZXJ0eUxpc3QtMS4wLmR0ZCI+CjxwbGlzdCB2ZXJzaW9uPSIxLjAiPjxkaWN0PjxrZXk+Q29tbWFuZDwva2V5PjxkaWN0PjxrZXk+UElOPC9rZXk+PHN0cmluZz4xMjM0NTY8L3N0cmluZz48a2V5PlJlcXVlc3RUeXBlPC9rZXk+PHN0cmluZz5EZXZpY2VMb2NrPC9zdHJpbmc+PC9kaWN0PjxrZXk+Q29tbWFuZFVVSUQ8L2tleT48c3RyaW5nPjc3ODFiNTNhLTZhMzUtNGIyZC05NDBlLWZiOGU1N2RhZGJhNTwvc3RyaW5nPjwvZGljdD48L3BsaXN0Pgo="
    }
  ]
}`

func TestCheckForExistingCommands(t *testing.T) {

	ok, err := checkForExistingCommand([]byte(testBodyDeviceLock), "DeviceLock")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !ok {
		t.Errorf("Expected DeviceLock command to be found, but it was not")
	}

	ok, err = checkForExistingCommand([]byte(testBodyDeviceLock), "EraseDevice")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if ok {
		t.Errorf("Expected EraseDevice command to not be found")
	}

}
