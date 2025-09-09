package com.xinchentechnote.designpatterns;

import org.junit.Test;
import static org.junit.Assert.assertEquals;

public class Crc32ChecksumServiceTest {
    @Test
    public void testCompute() {
        ChecksumService<byte[], Integer> service = new Crc32ChecksumService();
        int compute = service.compute("123456789".getBytes());
        assertEquals(0xCBF43926, compute);
    }

}
