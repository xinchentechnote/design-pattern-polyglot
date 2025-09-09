package com.xinchentechnote.designpatterns;

public interface ChecksumService<T, R> {
    // 算法名称
    String algorithm();
    // 计算校验和
    R compute(T input);
}
