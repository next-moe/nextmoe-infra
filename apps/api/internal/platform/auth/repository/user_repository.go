package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMoemoepointInsufficient = errors.New("moemoepoint balance insufficient")

var userSortColumns = map[string]string{
	"created_at":  "created_at",
	"name":        "name",
	"email":       "email",
	"moemoepoint": "moemoepoint",
}

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) FindByID(ctx context.Context, id uint) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByUUID(ctx context.Context, uuid string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByUUIDWithRoles(ctx context.Context, uuid string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Preload("Roles").Where("uuid = ?", uuid).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) AddRole(ctx context.Context, userID uint, roleName string) error {
	return r.db.WithContext(ctx).Exec(
		`INSERT INTO user_roles (user_id, role_id)
		 SELECT ?, id FROM roles WHERE name = ?
		 ON CONFLICT DO NOTHING`, userID, roleName).Error
}

func (r *UserRepository) RemoveRole(ctx context.Context, userID uint, roleName string) error {
	return r.db.WithContext(ctx).Exec(
		`DELETE FROM user_roles
		 WHERE user_id = ? AND role_id = (SELECT id FROM roles WHERE name = ?)`,
		userID, roleName).Error
}

func (r *UserRepository) FindByIDWithRoles(ctx context.Context, id uint) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Preload("Roles").First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) SearchByName(ctx context.Context, query string, limit int) ([]model.User, error) {
	escaped := escapeLikePattern(query)
	substring := "%" + escaped + "%"
	prefix := escaped + "%"

	var users []model.User
	err := r.db.WithContext(ctx).
		Preload("Roles").
		Where("name ILIKE ?", substring).
		Clauses(clause.OrderBy{
			Expression: clause.Expr{
				SQL: "CASE " +
					"WHEN LOWER(name) = LOWER(?) THEN 0 " +
					"WHEN name ILIKE ? THEN 1 " +
					"ELSE 2 END, name ASC",
				Vars: []any{query, prefix},
			},
		}).
		Limit(limit).
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (r *UserRepository) FindByIDsWithRoles(ctx context.Context, ids []uint) ([]model.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var users []model.User
	if err := r.db.WithContext(ctx).
		Preload("Roles").
		Where("id IN ?", ids).
		Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("LOWER(email) = ?", model.NormalizeEmail(email)).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByName(ctx context.Context, name string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *UserRepository) Update(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *UserRepository) CountByAvatarHash(ctx context.Context, hash string, excludeID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&model.User{}).
		Where("avatar_image_hash = ? AND id <> ?", hash, excludeID).
		Count(&n).Error
	return n, err
}

func (r *UserRepository) UpdatePassword(ctx context.Context, uuid string, password string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("uuid = ?", uuid).Update("password", password).Error
}

func (r *UserRepository) MigrateLegacyPassword(ctx context.Context, userID uint, newPasswordHash string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password":        newPasswordHash,
		"kungal_password": nil,
		"moyu_password":   nil,
	}).Error
}

func (r *UserRepository) UpdateEmail(ctx context.Context, uuid string, email string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("uuid = ?", uuid).Update("email", email).Error
}

func (r *UserRepository) UpdateProfile(ctx context.Context, uuid string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&model.User{}).
		Where("uuid = ?", uuid).
		Updates(fields).Error
}

// RenameWithCharge renames the user and charges `cost` moemoepoints in one
// transaction. Returns ErrMoemoepointInsufficient when the balance cannot cover
// the cost; a no-op (nil) when the name is unchanged.
func (r *UserRepository) RenameWithCharge(ctx context.Context, uuid, newName string, cost int) error {
	var user model.User
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("uuid = ?", uuid).First(&user).Error; err != nil {
			return err
		}
		if user.Name == newName {
			return nil
		}
		if user.Moemoepoint < cost {
			return ErrMoemoepointInsufficient
		}
		if err := tx.Model(&model.User{}).Where("id = ?", user.ID).
			Updates(map[string]any{"name": newName, "moemoepoint": user.Moemoepoint - cost}).Error; err != nil {
			return err
		}
		return tx.Create(&model.MoemoepointLog{
			UserID:         user.ID,
			Delta:          -cost,
			Reason:         model.MoemoepointReasonNameChange,
			SourceApp:      "oauth",
			ActorUserID:    user.ID,
			IdempotencyKey: "name_change:" + uuid + ":" + strconv.FormatInt(time.Now().UnixNano(), 10),
		}).Error
	})
}

func (r *UserRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.User{}, id).Error
}

func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]model.User, int64, error) {
	var users []model.User
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := r.db.WithContext(ctx).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

func (r *UserRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).Where("LOWER(email) = ?", model.NormalizeEmail(email)).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *UserRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *UserRepository) ExistsByEmailExcluding(ctx context.Context, email, excludeUUID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("LOWER(email) = ? AND uuid != ?", model.NormalizeEmail(email), excludeUUID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *UserRepository) ExistsByNameExcluding(ctx context.Context, name, excludeUUID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("name = ? AND uuid != ?", name, excludeUUID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *UserRepository) FindAllPaginated(
	ctx context.Context,
	page, limit int,
	search string,
	status *int,
	sortBy string,
	sortDesc bool,
) ([]model.User, int64, error) {
	var users []model.User
	var total int64

	query := r.db.WithContext(ctx).Model(&model.User{})

	if search != "" {
		like := "%" + escapeLikePattern(search) + "%"
		query = query.Where("name ILIKE ? OR email ILIKE ?", like, like)
	}

	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	col, ok := userSortColumns[sortBy]
	if !ok {
		col = "created_at"
	}
	query = query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: col},
		Desc:   sortDesc,
	})

	offset := (page - 1) * limit
	if err := query.Preload("Roles").Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}
