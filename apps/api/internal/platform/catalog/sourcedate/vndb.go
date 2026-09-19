package sourcedate

func VNDB(released int64, maxYear int) Verdict {
	if released == 99999999 {
		return Verdict{State: TBA}
	}
	yy := released / 10000
	if yy < minYear || yy > int64(maxYear) {
		return Verdict{State: Unknown}
	}
	v := Verdict{State: Dated, Y: int16(yy)}
	if mm := (released / 100) % 100; mm >= 1 && mm <= 12 {
		mv := int16(mm)
		v.M = &mv
		if dd := released % 100; dd >= 1 && dd <= 31 {
			dv := int16(dd)
			v.D = &dv
		}
	}
	return v
}
