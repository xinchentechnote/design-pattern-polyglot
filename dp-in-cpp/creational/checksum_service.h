#pragma once
#include <memory>
#include <string>
#include <unordered_map>
#include <vector>

class IChecksumService {
public:
  virtual std::string algorithm() const = 0;

  virtual uint64_t compute(const std::vector<uint8_t> &input) const = 0;

  virtual ~IChecksumService() = default;
};

class Crc16ChecksumService : public IChecksumService {
public:
  std::string algorithm() const override { return "CRC16"; }

  uint64_t compute(const std::vector<uint8_t> &input) const override {
    uint16_t crc = 0xFFFF;
    for (uint8_t b : input) {
      crc ^= b;
      for (int i = 0; i < 8; ++i) {
        if (crc & 0x0001) {
          crc = (crc >> 1) ^ 0xA001;
        } else {
          crc >>= 1;
        }
      }
    }
    return crc;
  }
};

class Crc32ChecksumService : public IChecksumService {
public:
  std::string algorithm() const override { return "CRC32"; }

  uint64_t compute(const std::vector<uint8_t> &input) const override {
    uint32_t crc = 0xFFFFFFFF;
    for (uint8_t b : input) {
      crc ^= b;
      for (int i = 0; i < 8; ++i) {
        if (crc & 1) {
          crc = (crc >> 1) ^ 0xEDB88320;
        } else {
          crc >>= 1;
        }
      }
    }
    return ~crc;
  }
};

class ChecksumServiceFactory {
public:
  static ChecksumServiceFactory &getInstance() { return instance_; }

  bool registerService(std::shared_ptr<IChecksumService> service) {
    auto algorithm = service->algorithm();
    if (services_.find(algorithm) != services_.end()) {
      return false;
    }
    services_[algorithm] = service;
    return true;
  }

  bool updateService(std::shared_ptr<IChecksumService> service) {
    auto algorithm = service->algorithm();
    if (services_.find(algorithm) == services_.end()) {
      return false;
    }
    services_[algorithm] = service;
    return true;
  }

  bool registerOrUpdateService(std::shared_ptr<IChecksumService> service) {
    if (registerService(service)) {
      return true;
    }
    return updateService(service);
  }

  bool unregisterService(const std::string &algorithm) {
    if (services_.find(algorithm) == services_.end()) {
      return false;
    }
    services_.erase(algorithm);
    return true;
  }

  void clear() { services_.clear(); }

  std::shared_ptr<IChecksumService> get(const std::string &algorithm) {
    return services_[algorithm];
  }

private:
  static ChecksumServiceFactory instance_;
  ChecksumServiceFactory() {
    services_["CRC16"] = std::make_shared<Crc16ChecksumService>();
    services_["CRC32"] = std::make_shared<Crc32ChecksumService>();
  }
  ChecksumServiceFactory(const ChecksumServiceFactory &) = delete;
  ChecksumServiceFactory &operator=(const ChecksumServiceFactory &) = delete;
  std::unordered_map<std::string, std::shared_ptr<IChecksumService>> services_;
};
