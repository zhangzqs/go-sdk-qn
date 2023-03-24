package operation

import (
	"context"
	"github.com/service-sdk/go-sdk-qn/api.v7/kodo"
	"github.com/service-sdk/go-sdk-qn/x/goroutine_pool.v7"
	"sync"
)

type deleteAsDeleteKeysWithRetries struct {
	l                  *singleClusterLister
	pool               *goroutine_pool.GoroutinePool
	paths              []string
	retries            uint
	retried            uint
	failedPath         []string
	failedPathIndexMap []int
	failedRsHosts      map[string]struct{}
	failedRsHostsLock  sync.RWMutex
	ctx                context.Context
	errors             []*DeleteKeysError

	// 动作与动作最大重试次数
	action           func(bucket kodo.Bucket, paths []string) ([]kodo.BatchItemRet, error)
	actionMaxRetries uint

	// 分批处理的大小与并发数
	batchSize        int
	batchConcurrency int
}

func newDeleteAsDeleteKeysWithRetries(
	ctx context.Context,
	l *singleClusterLister,
	paths []string,
	retries uint,
	action func(bucket kodo.Bucket, paths []string) ([]kodo.BatchItemRet, error),
	actionMaxRetries uint,

	// 分批处理的大小与并发数
	batchSize int,
	batchConcurrency int,
) *deleteAsDeleteKeysWithRetries {
	// 并发数计算
	concurrency := (len(paths) + l.batchSize - 1) / l.batchSize
	if concurrency > l.batchConcurrency {
		concurrency = l.batchConcurrency
	}
	return &deleteAsDeleteKeysWithRetries{
		ctx:              ctx,
		l:                l,
		pool:             goroutine_pool.NewGoroutinePool(concurrency),
		paths:            paths,
		retries:          retries,
		failedRsHosts:    make(map[string]struct{}),
		errors:           make([]*DeleteKeysError, len(paths)),
		action:           action,
		actionMaxRetries: actionMaxRetries,
		batchSize:        batchSize,
		batchConcurrency: batchConcurrency,
	}
}

// 执行一次动作
func (d *deleteAsDeleteKeysWithRetries) doActionOnce(paths []string) ([]kodo.BatchItemRet, error) {
	// 获取一个 rs host
	d.failedRsHostsLock.RLock()
	defer d.failedRsHostsLock.RUnlock()
	host := d.l.nextRsHost(d.failedRsHosts)

	// 根据拿到的rs域名构造一个 bucket
	bucket := d.l.newBucket(host, "", "")

	// 执行相应批处理操作
	r, err := d.action(bucket, paths)

	// 成功退出
	if err == nil {
		succeedHostName(host)
		return r, nil
	}
	// 出错了，获取写锁，记录错误的 rs host
	d.failedRsHostsLock.Lock()
	defer d.failedRsHostsLock.Unlock()
	d.failedRsHosts[host] = struct{}{}

	failHostName(host)
	return nil, err
}

func (d *deleteAsDeleteKeysWithRetries) doAction(paths []string) (r []kodo.BatchItemRet, err error) {
	for i := uint(0); i < d.actionMaxRetries; i++ {
		r, err = d.doActionOnce(paths)
		// 没有错误，直接返回
		if err == nil {
			return r, nil
		}
	}
	return nil, err
}

func (d *deleteAsDeleteKeysWithRetries) pushAllTaskToPool() {
	// 将任务进行分批处理
	for i := 0; i < len(d.paths); i += d.l.batchSize {
		// 计算本次批量处理的数量
		size := d.l.batchSize
		if size > len(d.paths)-i {
			size = len(d.paths) - i
		}

		// paths 是这批要删除的文件
		// index 是这批文件的起始位置
		func(paths []string, index int) {
			d.pool.Go(func(ctx context.Context) error {
				// 删除这一批文件，返回成功删除的结果
				res, _ := d.doAction(paths)

				// 批量删除的结果，过滤掉成功的，记录失败的到 errors 中
				for j, v := range res {
					if v.Code == 200 {
						d.errors[index+j] = nil
						continue
					}
					d.errors[index+j] = &DeleteKeysError{
						Name:  paths[j],
						Error: v.Error,
						Code:  v.Code,
					}
					elog.Warn("delete bad file:", paths[j], "with code:", v.Code)
				}
				return nil
			})
		}(d.paths[i:i+size], i)
	}
}

func (d *deleteAsDeleteKeysWithRetries) waitAllTask() error {
	// 等待所有的批量删除任务完成，如果出错了，直接结束返回错误
	return d.pool.Wait(d.ctx)
}

func (d *deleteAsDeleteKeysWithRetries) doAndRetryAction() ([]*DeleteKeysError, error) {
	// 把所有的批量删除任务放到 goroutine pool 中
	d.pushAllTaskToPool()

	// 等待所有的批量删除任务完成
	if err := d.waitAllTask(); err != nil {
		return nil, err
	}

	// 剩余重试次数为0，直接返回结果
	if d.retries <= 0 {
		return d.errors, nil
	}

	// 如果期望重试的次数大于0，那么尝试一次重试
	for i, e := range d.errors {
		// 对于所有的5xx错误，都记录下来，等待重试
		if e != nil && e.Code/100 == 5 {
			d.failedPathIndexMap = append(d.failedPathIndexMap, i)
			d.failedPath = append(d.failedPath, e.Name)
			elog.Warn("redelete bad file:", e.Name, "with code:", e.Code)
		}
	}

	// 如果有需要重试的文件，那么进行重试
	if len(d.failedPath) > 0 {
		elog.Warn("redelete ", len(d.failedPath), " bad files, retried:", d.retried)

		// 将失败的文件进行重试
		d.retries--
		d.retried++
		retriedErrors, err := d.doAndRetryAction()

		// 如果重试出错了，直接返回错误
		if err != nil {
			return d.errors, err
		}

		// 如果重试成功了，那么把重试的结果合并到原来的结果中
		for i, retriedError := range retriedErrors {
			d.errors[d.failedPathIndexMap[i]] = retriedError
		}
	}
	// 返回所有的错误
	return d.errors, nil
}
