package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func (p Publisher) writeReport(report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(p.Data, p.reportPath()), data)
}

func (p Publisher) readReport() (Report, error) {
	var report Report
	data, err := os.ReadFile(filepath.Join(p.Data, p.reportPath()))
	if err != nil {
		return report, err
	}
	err = json.Unmarshal(data, &report)
	return report, err
}

// LastReport also handles a process exiting between the start and finish records.
// Recheck under the writer's file lock before marking that run interrupted.
func (p Publisher) LastReport() (Report, error) {
	report, err := p.readReport()
	if err != nil || report.State != "running" {
		return report, err
	}
	lock, err := os.OpenFile(filepath.Join(p.Data, p.lockPath()), os.O_RDWR, 0)
	if err != nil {
		return Report{}, err
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		return report, nil
	}
	defer unlock()
	report, err = p.readReport()
	if err != nil || report.State != "running" {
		return report, err
	}
	report.State = "failed"
	report.Error = "上次发布中断，未收到完成记录，请重新发布。"
	report.Finished = time.Now()
	return report, p.writeReport(report)
}
