package storage

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SavePushNames saves multiple push names in a batch transaction
func (s *MessageStore) SavePushNames(pushNames map[string]string) error {
	if len(pushNames) == 0 {
		return nil
	}

	// Convert map to slice for batch insert
	var records []PushName
	for jid, name := range pushNames {
		records = append(records, PushName{
			JID:      jid,
			PushName: name,
		})
	}

	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "jid"}},
		DoUpdates: clause.AssignmentColumns([]string{"push_name", "updated_at"}),
	}).CreateInBatches(records, 100).Error
}

// GetPushName retrieves a single push name
func (s *MessageStore) GetPushName(jid string) (string, error) {
	var pn PushName
	err := s.db.Where("jid = ?", jid).First(&pn).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}

	return pn.PushName, nil
}

// LoadAllPushNames loads all push names into a map (for bulk processing)
func (s *MessageStore) LoadAllPushNames() (map[string]string, error) {
	var pushNames []PushName
	err := s.db.Find(&pushNames).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(pushNames))
	for _, pn := range pushNames {
		result[pn.JID] = pn.PushName
	}

	return result, nil
}
