package finance

import "errors"

const PlatformFee int64 = 20

func PaystackFee(amount int64) int64 {
	fee := amount * 15 / 1000
	if amount >= 250000 {
		fee += 100
	}
	if fee > 200000 {
		fee = 200000
	}
	return fee
}
func Checkout(fare int64, platformWaived bool) (total, platform, paystack int64) {
	platform = PlatformFee
	if platformWaived {
		platform = 0
	}
	paystack = PaystackFee(fare + platform)
	return fare + platform + paystack, platform, paystack
}
func Refund(fare int64, percent int) (int64, error) {
	if fare < 0 || (percent != 100 && percent != 80 && percent != 40) {
		return 0, errors.New("invalid refund")
	}
	return fare * int64(percent) / 100, nil
}
