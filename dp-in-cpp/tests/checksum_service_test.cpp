#include "creational/checksum_service.h"
#include "gtest/gtest.h"

TEST(ChecksumService, test_crc16) {
  std::shared_ptr<IChecksumService> service =
      std::make_shared<Crc16ChecksumService>();
  uint16_t checksum = service->compute(
      std::vector<uint8_t>{'1', '2', '3', '4', '5', '6', '7', '8', '9'});
  EXPECT_EQ(0x4B37u, checksum);
}

TEST(ChecksumService, test_crc32) {
  std::shared_ptr<IChecksumService> service =
      std::make_shared<Crc32ChecksumService>();
  uint32_t checksum = service->compute(
      std::vector<uint8_t>{'1', '2', '3', '4', '5', '6', '7', '8', '9'});
  EXPECT_EQ(0xCBF43926u, checksum);
}

TEST(ChecksumService, test_factory) {
  EXPECT_EQ(&ChecksumServiceFactory::getInstance(),
            &ChecksumServiceFactory::getInstance());
  auto crc16Service = ChecksumServiceFactory::getInstance().get("CRC16");
  EXPECT_NE(nullptr, crc16Service);
  EXPECT_EQ("CRC16", crc16Service->algorithm());
}