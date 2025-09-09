import pytest
import zlib
import crcmod
from checksum_service import *

@pytest.fixture
def factory():
    return ChecksumServiceFactory()


def test_singleton(factory):
    f1 = ChecksumServiceFactory()
    f2 = ChecksumServiceFactory()
    assert f1 is f2 
    assert f1 is factory 


def test_crc32(factory):
    data = b"hello world"
    expected = zlib.crc32(data)
    service = factory.get_service("CRC32")
    assert service.compute(data) == expected


def test_crc16(factory):
    data = b"hello world"
    crc16_func = crcmod.predefined.mkPredefinedCrcFun("crc-16")
    expected = crc16_func(data)
    service = factory.get_service("CRC16")
    assert service.compute(data) == expected


class DummyChecksumService(ChecksumService):
    def algorithm(self) -> str:
        return "DUMMY"
    def compute(self, input: bytes) -> int:
        return 12345


def test_register_and_get(factory):
    dummy = DummyChecksumService()
    factory.register(dummy)
    service = factory.get_service("DUMMY")
    assert service.compute(b"test") == 12345


def test_unregister(factory):
    dummy = DummyChecksumService()
    factory.register(dummy)
    factory.unregister("DUMMY")
    with pytest.raises(ValueError):
        factory.get_service("DUMMY")


class DummyChecksumServiceV2(ChecksumService):
    def algorithm(self) -> str:
        return "DUMMY"
    def compute(self, input: bytes) -> int:
        return 99999


def test_update(factory):
    factory.register(DummyChecksumService())
    factory.update(DummyChecksumServiceV2())
    service = factory.get_service("DUMMY")
    assert service.compute(b"test") == 99999


def test_register_or_update(factory):
    factory.register_or_update(DummyChecksumService())
    s1 = factory.get_service("DUMMY")
    assert s1.compute(b"x") == 12345

    factory.register_or_update(DummyChecksumServiceV2())
    s2 = factory.get_service("DUMMY")
    assert s2.compute(b"x") == 99999


def test_get_service_invalid(factory):
    with pytest.raises(ValueError):
        factory.get_service("NOT_EXIST")
