package main

import (
	"sync"
	"time"
)

var roleQueue = make(chan func(), 1000)
var once sync.Once

func initRoleWorker() {
	once.Do(func() {
		go func() {
			for job := range roleQueue {
				job()
				time.Sleep(250 * time.Millisecond) // RATE LIMIT SAFE
			}
		}()
	})
}

func queueRoleJob(job func()) {
	initRoleWorker()
	roleQueue <- job
}
