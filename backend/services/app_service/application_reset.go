package appservice

// CancelUpdateForReset waits for the current update operation without closing
// the controller. A failed reset must leave the updater available for use.
// ResetApplication serializes this with new operation entry points.
func CancelUpdateForReset(service *AppService) {
	controller := service.updates
	if controller == nil {
		return
	}
	controller.mu.Lock()
	operation := controller.operation
	if operation != nil {
		operation.cancel()
	}
	controller.mu.Unlock()
	if operation != nil {
		<-operation.done
	}
}
