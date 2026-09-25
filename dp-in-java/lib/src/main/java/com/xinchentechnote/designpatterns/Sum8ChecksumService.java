package com.xinchentechnote.designpatterns;

public class Sum8ChecksumService implements ChecksumService<byte[], Integer> {
    @Override
    public String algorithm() {
        return "SUM8";
    }

    @Override
    public Integer compute(byte[] input) {
        int sum = 0;
        for (byte b : input) {
            sum += (b & 0xFF);
        }
        return sum % 256;
    }
}
