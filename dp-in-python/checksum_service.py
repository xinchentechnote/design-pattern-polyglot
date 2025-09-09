
from abc import ABCMeta, abstractmethod
import zlib
import crcmod


class Singleton(type):
    _instances = {}

    def __call__(cls, *args, **kwargs):
        # Singleton pattern implementation
        if cls not in cls._instances:
            cls._instances[cls] = super(Singleton, cls).__call__(*args, **kwargs)
        return cls._instances[cls]
    
    def get_instance(cls):
        if cls not in cls._instances:
            cls._instances[cls] = cls()
        return cls._instances[cls]

class ChecksumService(metaclass=ABCMeta):
    @abstractmethod
    def algorithm(self) -> str:
        pass

    @abstractmethod
    def compute(self, input: bytes) -> int:
        pass

class Crc16ChecksumService(ChecksumService):
    def algorithm(self) -> str:
        return "CRC16"

    def compute(self, input: bytes) -> int:

        crc16 = crcmod.predefined.mkPredefinedCrcFun('crc-16')
        return crc16(input)

class CRC32ChecksumService(ChecksumService):
    def algorithm(self) -> str:
        return "CRC32"

    def compute(self, input: bytes) -> int:
        return zlib.crc32(input)
    
class ChecksumServiceFactory(metaclass=Singleton):
    
    def __init__(self):
        self._services = {
            "CRC16": Crc16ChecksumService(),
            "CRC32": CRC32ChecksumService()
        }
        
    def get_service(self, algorithm: str) -> ChecksumService:
        service = self._services.get(algorithm.upper())
        if not service:
            raise ValueError(f"Unsupported algorithm: {algorithm}")
        return service
    
    def register(self,service: ChecksumService):
        self._services[service.algorithm().upper()] = service
    
    def unregister(self,algorithm: str):
        if algorithm.upper() in self._services:
            del self._services[algorithm.upper()]
    
    def update(self,service: ChecksumService):
        self.unregister(service.algorithm())
        self.register(service)
    
    def register_or_update(self,service: ChecksumService):
        if service.algorithm().upper() in self._services:
            self.update(service)
        else:
            self.register(service)