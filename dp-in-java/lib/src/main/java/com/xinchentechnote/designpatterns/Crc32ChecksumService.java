package com.xinchentechnote.designpatterns;

public class Crc32ChecksumService implements ChecksumService<byte[], Integer> {

    private static final int[] CRC_TABLE = new int[256];

    static {
        for (int i = 0; i < 256; i++) {
            int c = i;
            for (int j = 0; j < 8; j++) {
                if ((c & 1) != 0) {
                    c = 0xEDB88320 ^ (c >>> 1);
                } else {
                    c >>>= 1;
                }
            }
            CRC_TABLE[i] = c;
        }
    }

    @Override
    public String algorithm() {
        return "CRC32";
    }

    @Override
    public Integer compute(byte[] input) {
        int crc = 0xFFFFFFFF;

        for (int i = 0; i < input.length; i++) {
            crc = CRC_TABLE[(crc ^ input[i]) & 0xFF] ^ (crc >>> 8);
        }

        return crc ^ 0xFFFFFFFF;
    }
}
