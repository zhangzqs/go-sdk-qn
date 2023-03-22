package test

import (
	"github.com/service-sdk/go-sdk-qn/syncdata/operation"
	"os"
	"strings"
	"testing"
)

var (
	skipTest = true
	config   *operation.Config
	lister   *operation.Lister
	uploader *operation.Uploader
)

func setup() {
	skipTest = os.Getenv("QINIU_KODO_TEST") == ""
	config = &operation.Config{
		Ak:      os.Getenv("QINIU_ACCESS_KEY"),
		Sk:      os.Getenv("QINIU_SECRET_KEY"),
		UpHosts: strings.Split(os.Getenv("QINIU_TEST_UP_HOSTS"), ","),
		RsfHosts: append(
			strings.Split(os.Getenv("QINIU_TEST_RSF_HOSTS"), ","),
			os.Getenv("QINIU_TEST_RSF_HOST"),
		),
		RsHosts: append(
			strings.Split(os.Getenv("QINIU_TEST_RS_HOSTS"), ","),
			os.Getenv("QINIU_TEST_RS_HOST"),
		),
		ApiServerHosts: append(
			strings.Split(os.Getenv("QINIU_TEST_API_HOSTS"), ","),
			os.Getenv("QINIU_TEST_API_HOST"),
		),
		IoHosts: append(
			strings.Split(os.Getenv("QINIU_TEST_IO_HOSTS"), ","),
			os.Getenv("QINIU_TEST_IO_HOST"),
		),
		UcHosts: append(
			strings.Split(os.Getenv("QINIU_TEST_UC_HOSTS"), ","),
			os.Getenv("QINIU_TEST_UC_HOST"),
		),
		Bucket: os.Getenv("QINIU_TEST_BUCKET"),
		//RecycleBin: "recycle",
	}
	lister = operation.NewLister(config)
	uploader = operation.NewUploader(config)
}

func TestMain(t *testing.M) {
	setup()
	os.Exit(t.Run())
}

// 检查是否应该跳过测试
func checkSkipTest(t *testing.T) {
	if skipTest {
		t.Skip("skipping test in short mode.")
	}
}

func bucketName() string {
	return config.Bucket
}
