package com.xinchentechnote.designpatterns;

public class Crc16ChecksumService implements ChecksumService<byte[], Integer> {
    @Override
    public String algorithm() {
        return "CRC16";
    }

    @Override
    public Integer compute(byte[] input) {
        int crc = 0xFFFF;
        for (byte b : input) {
            crc ^= (b & 0xFF);
            for (int i = 0; i < 8; i++) {
                if ((crc & 1) != 0) {
                    crc = (crc >>> 1) ^ 0xA001;
                } else {
                    crc = crc >>> 1;
                }
            }
        }
        return crc & 0xFFFF;
    }

}
