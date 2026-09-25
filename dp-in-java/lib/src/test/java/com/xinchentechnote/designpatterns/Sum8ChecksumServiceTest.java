package com.xinchentechnote.designpatterns;

import org.junit.Test;
import static org.junit.Assert.assertEquals;

public class Sum8ChecksumServiceTest {

    @Test
    public void testCompute() {
        Sum8ChecksumService sum8ChecksumService = new Sum8ChecksumService();
        int result = sum8ChecksumService.compute("123456789".getBytes());
        assertEquals(result, 221);
    }

    @Test
    public void testComputeBinaryFrame() {
        byte[] frame = {(byte) 0xCA, 0x01, 0x00, 0x04, 0x54, 0x45, 0x53, 0x54};
        Sum8ChecksumService sum8ChecksumService = new Sum8ChecksumService();
        int result = sum8ChecksumService.compute(frame);
        assertEquals(result, 0x0F);
    }
}
