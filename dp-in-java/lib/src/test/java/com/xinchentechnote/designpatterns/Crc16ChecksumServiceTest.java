package com.xinchentechnote.designpatterns;

import org.junit.Test;
import static org.junit.Assert.assertEquals;

public class Crc16ChecksumServiceTest {

    @Test
    public void testCompute() {
        Crc16ChecksumService crc16ChecksumService = new Crc16ChecksumService();
        int result = crc16ChecksumService.compute("123456789".getBytes());
        assertEquals(result, 0x4B37);
    }
}
