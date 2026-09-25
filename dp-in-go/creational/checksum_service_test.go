package creational

import (
	"testing"
)

func TestCrc16(t *testing.T) {
	service := &Crc16ChecksumService{}
	data := []byte{'1', '2', '3', '4', '5', '6', '7', '8', '9'}
	result := service.Compute(data)
	expected := uint16(0x4B37)
	if result != expected {
		t.Errorf("CRC16 failed: got %X, want %X", result, expected)
	}
}

func TestCrc32(t *testing.T) {
	service := &Crc32ChecksumService{}
	data := []byte{'1', '2', '3', '4', '5', '6', '7', '8', '9'}
	result := service.Compute(data)
	expected := uint32(0xCBF43926)
	if result != expected {
		t.Errorf("CRC32 failed: got %X, want %X", result, expected)
	}
}

func TestSum8Checksum(t *testing.T) {
	service := &Sum8ChecksumService{}
	data := []byte{'1', '2', '3', '4', '5', '6', '7', '8', '9'}
	result := service.Compute(data)
	expected := uint8(221)
	if result != expected {
		t.Errorf("SUM8 checksum failed: got %d, want %d", result, expected)
	}
}

func TestFactorySingleton(t *testing.T) {
	f1 := GetChecksumServiceFactory()
	f2 := GetChecksumServiceFactory()
	if f1 != f2 {
		t.Error("Factory singleton failed: instances are not the same")
	}
}

func TestFactoryGetService(t *testing.T) {
	factory := GetChecksumServiceFactory()
	crc16 := factory.Get("CRC16")
	if crc16 == nil {
		t.Fatal("CRC16 service not found")
	}
	if crc16.(IChecksumService[[]byte,uint16]).Algorithm() != "CRC16" {
		t.Error("CRC16 service Algorithm mismatch")
	}

	crc32 := factory.Get("CRC32")
	if crc32 == nil {
		t.Fatal("CRC32 service not found")
	}
	if crc32.(IChecksumService[[]byte,uint32]).Algorithm() != "CRC32" {
		t.Error("CRC32 service Algorithm mismatch")
	}
}
