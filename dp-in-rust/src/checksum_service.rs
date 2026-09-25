use std::{
    collections::HashMap,
    sync::{Arc, RwLock},
};

use crc::{CRC_32_ISO_HDLC, Crc};
use once_cell::sync::Lazy;

pub enum Checksum {
    U8(u8),
    U16(u16),
    U32(u32),
    U64(u64),
    I8(i8),
    I16(i16),
    I32(i32),
    I64(i64),
}

pub trait ChecksumService: Send + Sync {
    fn algorithm(&self) -> &'static str;
    fn compute(&self, input: &[u8]) -> Checksum;
}

pub struct Crc16ChecksumService;

impl ChecksumService for Crc16ChecksumService {
    fn algorithm(&self) -> &'static str {
        "CRC16"
    }

    fn compute(&self, input: &[u8]) -> Checksum {
        let mut crc: u16 = 0xFFFF;
        for &b in input {
            crc ^= b as u16;
            for _ in 0..8 {
                if crc & 0x0001 != 0 {
                    crc = (crc >> 1) ^ 0xA001;
                } else {
                    crc >>= 1;
                }
            }
        }
        Checksum::U16(crc)
    }
}

pub struct Crc32ChecksumService;

impl ChecksumService for Crc32ChecksumService {
    fn algorithm(&self) -> &'static str {
        "CRC32"
    }

    fn compute(&self, input: &[u8]) -> Checksum {
        let crc = Crc::<u32>::new(&CRC_32_ISO_HDLC);
        let mut digest = crc.digest();
        digest.update(input);
        Checksum::U32(digest.finalize())
    }
}

/// 累加和校验（SUM8）：校验范围内所有字节求和后 mod 256，取低 8 位
pub struct Sum8ChecksumService;

impl ChecksumService for Sum8ChecksumService {
    fn algorithm(&self) -> &'static str {
        "SUM8"
    }

    fn compute(&self, input: &[u8]) -> Checksum {
        let sum = input.iter().map(|&b| b as u64).sum::<u64>();
        Checksum::U8((sum % 256) as u8)
    }
}

struct ChecksumServiceFactory {
    cache: RwLock<HashMap<&'static str, Arc<dyn ChecksumService>>>,
}

impl ChecksumServiceFactory {
    fn new() -> Self {
        Self {
            cache: RwLock::new(HashMap::new()),
        }
    }
    fn register(&self, service: Arc<dyn ChecksumService>) -> bool {
        let mut cache = self.cache.write().unwrap();
        if cache.contains_key(service.algorithm()) {
            false
        } else {
            cache.insert(service.algorithm(), service);
            true
        }
    }

    fn unregister(&self, algorithm: &str) -> bool {
        let mut cache = self.cache.write().unwrap();
        cache.remove(algorithm).is_some()
    }

    fn clear(&self) {
        let mut cache = self.cache.write().unwrap();
        cache.clear();
    }

    fn get(&self, algorithm: &str) -> Option<Arc<dyn ChecksumService>> {
        let cache = self.cache.read().unwrap();
        cache.get(algorithm).cloned()
    }
}

// Singleton factory instance
pub static CHECKSUM_SERVICE_FACTORY: Lazy<ChecksumServiceFactory> = Lazy::new(|| {
    let mut factory = ChecksumServiceFactory::new();
    factory.register(Arc::new(Sum8ChecksumService));
    factory.register(Arc::new(Crc16ChecksumService));
    factory.register(Arc::new(Crc32ChecksumService));
    factory
});

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_crc16_checksum_service() {
        let service = Arc::new(Crc16ChecksumService);
        assert_eq!(service.algorithm(), "CRC16");
        let checksum = service.compute(b"123456789");
        match checksum {
            Checksum::U16(value) => assert_eq!(value, 0x4B37),
            _ => panic!("Expected U16 checksum"),
        }
    }
    #[test]
    fn test_crc32_checksum_service() {
        let service = Arc::new(Crc32ChecksumService);
        assert_eq!(service.algorithm(), "CRC32");
        let checksum = service.compute(b"123456789");
        match checksum {
            Checksum::U32(value) => assert_eq!(value, 0xCBF43926),
            _ => panic!("Expected U32 checksum"),
        }
    }
    #[test]
    fn test_sum8_checksum_service() {
        let service = Sum8ChecksumService;
        assert_eq!(service.algorithm(), "SUM8");
        let checksum = service.compute(b"123456789");
        match checksum {
            Checksum::U8(value) => assert_eq!(value, 221),
            _ => panic!("Expected U8 checksum"),
        }
    }
    #[test]
    fn test_sum8_checksum_binary_frame() {
        // 二进制帧：CA 01 00 04 | 54 45 53 54 | <checksum>
        let frame: &[u8] = &[0xCA, 0x01, 0x00, 0x04, 0x54, 0x45, 0x53, 0x54];
        match Sum8ChecksumService.compute(frame) {
            Checksum::U8(value) => assert_eq!(value, 0x0F),
            _ => panic!("Expected U8 checksum"),
        }
    }
    #[test]
    fn test_checksum_service_factory() {
        let factory = ChecksumServiceFactory::new();
        let crc16_service = Arc::new(Crc16ChecksumService);
        let crc32_service = Arc::new(Crc32ChecksumService);

        assert!(factory.register(crc16_service.clone()));
        assert!(factory.register(crc32_service.clone()));
        assert!(!factory.register(crc16_service.clone())); // Duplicate registration

        let fetched_crc16 = factory.get("CRC16").unwrap();
        let fetched_crc32 = factory.get("CRC32").unwrap();

        assert_eq!(fetched_crc16.algorithm(), "CRC16");
        assert_eq!(fetched_crc32.algorithm(), "CRC32");

        assert!(factory.unregister("CRC16"));
        assert!(!factory.unregister("CRC16")); // Already unregistered

        factory.clear();
        assert!(factory.get("CRC32").is_none());
    }

    #[test]
    fn test_checksum_service_factory_with_multiple_threads() {
        use std::thread;

        let factory = Arc::new(ChecksumServiceFactory::new());
        let crc16_service = Arc::new(Crc16ChecksumService);
        let crc32_service = Arc::new(Crc32ChecksumService);

        let factory_clone1 = factory.clone();
        let handle1 = thread::spawn(move || {
            assert!(factory_clone1.register(crc16_service));
        });

        let factory_clone2 = factory.clone();
        let handle2 = thread::spawn(move || {
            assert!(factory_clone2.register(crc32_service));
        });

        handle1.join().unwrap();
        handle2.join().unwrap();

        let fetched_crc16 = factory.get("CRC16").unwrap();
        let fetched_crc32 = factory.get("CRC32").unwrap();

        assert_eq!(fetched_crc16.algorithm(), "CRC16");
        assert_eq!(fetched_crc32.algorithm(), "CRC32");
    }
}
