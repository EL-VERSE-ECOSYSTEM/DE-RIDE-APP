package roles

import "errors"

type Role string

const (
	Passenger     Role = "PASSENGER"
	Driver        Role = "DRIVER"
	Dispatcher    Role = "DISPATCHER"
	Biker         Role = "BIKER"
	DispatchRider Role = "DISPATCH_RIDER"
	ParkOperator  Role = "PARK_OPERATOR"
	Admin         Role = "ADMIN"
)

func CanBuyPlan(role Role, oneTime60000 bool) error {
	if oneTime60000 && role != Passenger {
		return errors.New("ERR_ROLE_PLAN_RESTRICTED")
	}
	return nil
}
