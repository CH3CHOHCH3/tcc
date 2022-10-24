# TCC

在作者学习编译原理的过程中，使用 golang 为<a href="https://pandolia.net/tinyc/ch2_TinyC_syntax.html"> TinyC </a>开发的一个编译器。目标平台是 RISC-V 32，生成的汇编代码可以在<a href="https://github.com/TheThirdOne/rars"> RARS </a>上运行。

## 前端

### 词法分析

词法分析的实现基于 <a href="https://github.com/blynn/nex">Nex</a>，类似于 flex，但生成代码是 go。