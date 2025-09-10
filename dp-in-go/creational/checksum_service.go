package creational

import (
	"hash/crc32"
	"sync"
)

type IChecksumService[T any, R any] interface {
	Algorithm() string
	Compute(data T) R
}

type Crc16ChecksumService struct {
}

func (c *Crc16ChecksumService) Algorithm() string {
	return "CRC16"
}

func (c *Crc16ChecksumService) Compute(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if (crc & 0x0001) != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc = crc >> 1
			}
		}
	}
	return crc
}

type Crc32ChecksumService struct {
}

func (c *Crc32ChecksumService) Algorithm() string {
	return "CRC32"
}

func (c *Crc32ChecksumService) Compute(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

type checksumServiceFactory struct {
	services map[string]any
}

var (
	instance *checksumServiceFactory
	once     sync.Once
)

func GetChecksumServiceFactory() *checksumServiceFactory {
	once.Do(func() {
		instance = &checksumServiceFactory{
			services: make(map[string]any),
		}
		instance.Register(&Crc16ChecksumService{})
		instance.Register(&Crc32ChecksumService{})
	})
	return instance
}

func (f *checksumServiceFactory) Register(service any) {
	switch s := service.(type) {
	case *Crc16ChecksumService:
		f.services[s.Algorithm()] = s
	case *Crc32ChecksumService:
		f.services[s.Algorithm()] = s
	default:
		panic("unsupported checksum service type")
	}
}

func (f *checksumServiceFactory) Get(name string) any {
	return f.services[name]
}
