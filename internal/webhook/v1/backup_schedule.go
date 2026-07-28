package v1

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

// ValidateFortigateBackupSpec verifica la coerenza tra Frequency e i campi
// che dipendono da essa (At, DayOfWeek, DayOfMonth). Restituisce TUTTI i
// problemi trovati in un colpo solo (field.ErrorList) - non si ferma al
// primo, così chi corregge una CR sbagliata li vede tutti insieme invece
// di scoprirli un errore alla volta riapplicando più volte.
//
// Regole:
//   - Hourly:  At deve essere vuoto. DayOfWeek/DayOfMonth non sono
//     vincolati (nessun controllo in nessuna delle due direzioni).
//   - Daily:   At obbligatorio. DayOfWeek e DayOfMonth vietati.
//   - Weekly:  At e DayOfWeek obbligatori. DayOfMonth vietato.
//   - Monthly: At e DayOfMonth obbligatori. DayOfWeek vietato.
//
// Weekly e la regola "At" per Monthly non erano specificate esplicitamente
// nella richiesta originale - dedotte per coerenza con Daily. Se non è il
// comportamento voluto, va corretta qui.
func ValidateFortigateBackupSpec(spec *k8sdinovaonev1.FortigateBackupSpec, fldPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	atPath := fldPath.Child("at")
	dayOfWeekPath := fldPath.Child("dayOfWeek")
	dayOfMonthPath := fldPath.Child("dayOfMonth")

	switch spec.Frequency {
	case "Hourly":
		if spec.At != "" {
			errs = append(errs, field.Forbidden(atPath, `non deve essere impostato quando frequency è "Hourly"`))
		}

	case "Daily":
		if spec.At == "" {
			errs = append(errs, field.Required(atPath, `obbligatorio quando frequency è "Daily"`))
		}
		if spec.DayOfWeek != "" {
			errs = append(errs, field.Forbidden(dayOfWeekPath, `non deve essere impostato quando frequency è "Daily"`))
		}
		if spec.DayOfMonth != 0 {
			errs = append(errs, field.Forbidden(dayOfMonthPath, `non deve essere impostato quando frequency è "Daily"`))
		}

	case "Weekly":
		if spec.At == "" {
			errs = append(errs, field.Required(atPath, `obbligatorio quando frequency è "Weekly"`))
		}
		if spec.DayOfWeek == "" {
			errs = append(errs, field.Required(dayOfWeekPath, `obbligatorio quando frequency è "Weekly"`))
		}
		if spec.DayOfMonth != 0 {
			errs = append(errs, field.Forbidden(dayOfMonthPath, `non deve essere impostato quando frequency è "Weekly"`))
		}

	case "Monthly":
		if spec.At == "" {
			errs = append(errs, field.Required(atPath, `obbligatorio quando frequency è "Monthly"`))
		}
		if spec.DayOfMonth == 0 {
			errs = append(errs, field.Required(dayOfMonthPath, `obbligatorio quando frequency è "Monthly"`))
		}
		if spec.DayOfWeek != "" {
			errs = append(errs, field.Forbidden(dayOfWeekPath, `non deve essere impostato quando frequency è "Monthly"`))
		}

	default:
		errs = append(errs, field.Invalid(
			fldPath.Child("frequency"), spec.Frequency,
			`deve essere uno tra "Hourly", "Daily", "Weekly", "Monthly"`,
		))
	}

	return errs
}