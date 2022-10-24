package main

import (
	"flag"
	"fmt"
	"tcc/pkg/LA"
)

func main() {
	// 从参数中获取源文件路径和目标汇编文件名
	var srcPath string
	var asmPath string
	flag.StringVar(&srcPath, "src", "a.tc", "Path of source file")
	flag.StringVar(&asmPath, "asm", "a.asm", "Name of asm file")
	flag.Parse()

	// 对源码进行词法分析，获取 token 列表
	tokenList := LA.Parse(srcPath)

	for i := tokenList.Front(); i != nil; i = i.Next() {
		fmt.Println(i.Value)
	}
}
