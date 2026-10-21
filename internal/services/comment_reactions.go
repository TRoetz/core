package services

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"windshift/internal/models"
	"windshift/internal/repository"
)

// CommentReactionKeys is the fixed set of reactions a user can leave on a
// comment, in display order (core#298). Keys, not emoji, travel over the API
// so clients render them consistently.
var CommentReactionKeys = []string{"thumbs_up", "check", "eyes", "heart", "laugh", "hooray"}

// ErrInvalidCommentReaction reports a reaction key outside CommentReactionKeys.
var ErrInvalidCommentReaction = errors.New("reaction is not supported")

// maxReactionCommentIDs bounds one IN clause; comment feeds return at most
// MaxCommentFeedLimit rows per item, but batches span many items.
const maxReactionCommentIDs = 500

// ValidCommentReaction reports whether key is a supported reaction.
func ValidCommentReaction(key string) bool {
	return slices.Contains(CommentReactionKeys, key)
}

// AddReaction records userID's reaction on a comment. Adding the same
// reaction twice is a no-op.
func (s *CommentService) AddReaction(commentID, userID int, reaction string) ([]models.CommentReaction, error) {
	if !ValidCommentReaction(reaction) {
		return nil, ErrInvalidCommentReaction
	}
	itemID, err := s.reactionCommentItem(commentID)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecWrite(`
		INSERT INTO comment_reactions (comment_id, user_id, reaction) VALUES (?, ?, ?)
		ON CONFLICT (comment_id, user_id, reaction) DO NOTHING`, commentID, userID, reaction); err != nil {
		return nil, fmt.Errorf("add reaction to comment %d: %w", commentID, err)
	}
	PublishItemChange(itemID)
	return s.ReactionsForComment(commentID)
}

// RemoveReaction withdraws userID's reaction from a comment. Removing a
// reaction that does not exist is a no-op.
func (s *CommentService) RemoveReaction(commentID, userID int, reaction string) error {
	if !ValidCommentReaction(reaction) {
		return ErrInvalidCommentReaction
	}
	itemID, err := s.reactionCommentItem(commentID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecWrite(`DELETE FROM comment_reactions WHERE comment_id = ? AND user_id = ? AND reaction = ?`,
		commentID, userID, reaction); err != nil {
		return fmt.Errorf("remove reaction from comment %d: %w", commentID, err)
	}
	PublishItemChange(itemID)
	return nil
}

// ReactionsForComment returns the grouped reactions on one comment.
func (s *CommentService) ReactionsForComment(commentID int) ([]models.CommentReaction, error) {
	byComment, err := s.reactionsByComment([]int{commentID})
	if err != nil {
		return nil, err
	}
	reactions := byComment[commentID]
	if reactions == nil {
		reactions = []models.CommentReaction{}
	}
	return reactions, nil
}

// AttachReactions fills Reactions on the human comments in place. Approval
// rows (non-positive IDs) are skipped.
func (s *CommentService) AttachReactions(comments []models.Comment) error {
	ids := make([]int, 0, len(comments))
	for _, comment := range comments {
		if comment.ID > 0 && comment.Source != "approval" {
			ids = append(ids, comment.ID)
		}
	}
	byComment, err := s.reactionsByComment(ids)
	if err != nil {
		return err
	}
	for i := range comments {
		comments[i].Reactions = byComment[comments[i].ID]
	}
	return nil
}

func (s *CommentService) reactionCommentItem(commentID int) (int, error) {
	var itemID int
	err := s.db.QueryRow(`SELECT item_id FROM comments WHERE id = ?`, commentID).Scan(&itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("comment %d: %w", commentID, repository.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("find comment %d: %w", commentID, err)
	}
	return itemID, nil
}

func (s *CommentService) reactionsByComment(ids []int) (map[int][]models.CommentReaction, error) {
	result := make(map[int][]models.CommentReaction)
	for start := 0; start < len(ids); start += maxReactionCommentIDs {
		chunk := ids[start:min(start+maxReactionCommentIDs, len(ids))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.Query(fmt.Sprintf(`
			SELECT r.comment_id, r.reaction, r.user_id,
			       COALESCE(NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), u.username, 'Unknown User')
			FROM comment_reactions r
			JOIN users u ON u.id = r.user_id
			WHERE r.comment_id IN (%s)
			ORDER BY r.comment_id, r.created_at, r.id`, placeholders(len(chunk))), args...)
		if err != nil {
			return nil, fmt.Errorf("list comment reactions: %w", err)
		}
		for rows.Next() {
			var commentID, userID int
			var reaction, name string
			if err := rows.Scan(&commentID, &reaction, &userID, &name); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan comment reaction: %w", err)
			}
			result[commentID] = appendReaction(result[commentID], reaction, userID, name)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, fmt.Errorf("read comment reactions: %w", err)
		}
	}
	for id := range result {
		slices.SortStableFunc(result[id], func(a, b models.CommentReaction) int {
			return slices.Index(CommentReactionKeys, a.Reaction) - slices.Index(CommentReactionKeys, b.Reaction)
		})
	}
	return result, nil
}

func appendReaction(groups []models.CommentReaction, reaction string, userID int, name string) []models.CommentReaction {
	for i := range groups {
		if groups[i].Reaction == reaction {
			groups[i].Count++
			groups[i].UserIDs = append(groups[i].UserIDs, userID)
			groups[i].UserNames = append(groups[i].UserNames, name)
			return groups
		}
	}
	return append(groups, models.CommentReaction{Reaction: reaction, Count: 1, UserIDs: []int{userID}, UserNames: []string{name}})
}
