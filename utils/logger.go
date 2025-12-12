package utils

/*
#include <stdio.h>
#include <stdlib.h>

// 定义回调函数类型
typedef void (*CLogFunc)(const char* msg);

// 全局函数指针
static CLogFunc g_log_callback = NULL;

static void set_log_callback(CLogFunc cb) {
    g_log_callback = cb;
}

static void call_log_callback(const char* msg) {
    if (g_log_callback != NULL) {
        g_log_callback(msg);
    }else{
	printf("%s",msg);
	}
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

//export SetLogger
func SetLogger(fn uintptr) {
	C.set_log_callback((C.CLogFunc)(unsafe.Pointer(fn)))
}

// 安全释放C字符串
func freeCString(cstr *C.char) {
	if cstr != nil {
		C.free(unsafe.Pointer(cstr))
	}
}

// 添加level标签的日志输出
func logOutput(level, format string, args ...interface{}) {
	// 格式化消息
	var msg string
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	} else {
		msg = format
	}

	// 添加时间戳和调用者信息（可选）
	fullMsg := fmt.Sprintf("[%s] %s\n", level, msg)

	cMsg := C.CString(fullMsg)
	defer func() {
		freeCString(cMsg)
	}()

	// 调用C回调
	C.call_log_callback(cMsg)
}

// 增加更丰富的日志函数
func Debug(format string, args ...interface{}) {
	logOutput("DEBUG", format, args...)
}

func Info(format string, args ...interface{}) {
	logOutput("INFO", format, args...)
}

func Warn(format string, args ...interface{}) {
	logOutput("WARN", format, args...)
}

func Error(format string, args ...interface{}) {
	logOutput("ERROR", format, args...)
}
