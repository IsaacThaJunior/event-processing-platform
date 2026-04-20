package handler

func StripNext(task *TaskRequest) *TaskRequest {
	if task == nil {
		return nil
	}

	clean := *task
	clean.Next = nil

	return &clean
}
