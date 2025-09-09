package com.xinchentechnote.designpatterns;

import org.junit.Test;
import static org.junit.Assert.*;

public class ChecksumServiceFactoryTest {
    @Test
    public void testGet() {
        ChecksumServiceFactory factory = ChecksumServiceFactory.getInstance();
        ChecksumService<?, ?> service = factory.get("CRC32");
        assertEquals("CRC32", service.algorithm());
    }

    @Test
    public void testRegister() {
        ChecksumServiceFactory factory = ChecksumServiceFactory.getInstance();
        factory.clear();
        factory.register(new Crc32ChecksumService());
        ChecksumService<?, ?> service = factory.get("CRC32");
        assertEquals("CRC32", service.algorithm());
    }

    @Test
    public void testRegisterOrUpdate() {
        ChecksumServiceFactory factory = ChecksumServiceFactory.getInstance();
        factory.clear();
        factory.registerOrUpdate(new Crc32ChecksumService());
        ChecksumService<?, ?> service = factory.get("CRC32");
        assertEquals("CRC32", service.algorithm());
    }

    @Test
    public void testUnregister() {
        ChecksumServiceFactory factory = ChecksumServiceFactory.getInstance();
        factory.unregister("CRC32");
        ChecksumService<?, ?> service = factory.get("CRC32");
        assertEquals(null, service);

    }

    @Test
    public void testUpdate() {
        ChecksumServiceFactory factory = ChecksumServiceFactory.getInstance();
        assertFalse(factory.update(new Crc32ChecksumService()));

    }
}
