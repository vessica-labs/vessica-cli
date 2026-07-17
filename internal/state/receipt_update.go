package state

import (
	"context"
	"encoding/json"
)

func (db *DB) UpdateReceipt(ctx context.Context, receiptID, status string, body any) (*Receipt, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(ctx, `UPDATE receipts SET status=?, body_json=? WHERE id=?`, status, string(b), receiptID); err != nil {
		return nil, err
	}
	return db.GetReceipt(ctx, receiptID)
}
