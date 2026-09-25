package com.xinchentechnote.designpatterns;

import java.util.HashMap;
import java.util.Map;

public enum ChecksumServiceFactory {
    INSTANCE;

    private Map<String, ChecksumService<?, ?>> services = new HashMap<>();

    ChecksumServiceFactory() {
        register(new Sum8ChecksumService());
        register(new Crc16ChecksumService());
        register(new Crc32ChecksumService());
    }

    public static ChecksumServiceFactory getInstance() {
        return INSTANCE;
    }

    public boolean register(ChecksumService<?, ?> service) {
        if (services.containsKey(service.algorithm())) {
            return false;
        }
        services.put(service.algorithm(), service);
        return true;
    }

    public boolean unregister(String algorithm) {
        if (!services.containsKey(algorithm)) {
            return false;
        }
        services.remove(algorithm);
        return true;
    }

    public boolean update(ChecksumService<?, ?> service) {
        if (!services.containsKey(service.algorithm())) {
            return false;
        }
        services.put(service.algorithm(), service);
        return true;
    }

    public boolean registerOrUpdate(ChecksumService<?, ?> service) {
        return register(service) || update(service);
    }

    @SuppressWarnings("unchecked")
    public <T, R> ChecksumService<T, R> get(String algorithm) {
        return (ChecksumService<T, R>) services.get(algorithm);
    }

    public void clear() {
        services.clear();
    }
}
