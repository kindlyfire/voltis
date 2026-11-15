package db

// UTCJoin joins the user's user_to_content row utc to content c, for @user_id.
const UTCJoin = `
		LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id`

// ChildCountsJoin counts the valid children of content c in cc, for @user_id, with utc joined.
const ChildCountsJoin = `
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS children_count,
			COUNT(*) FILTER (WHERE child_utc.status = 'completed') AS completed_children_count,
			COUNT(*) FILTER (WHERE child_utc.status = 'dropped') AS dropped_children_count,
			COUNT(*) FILTER (WHERE utc.status = 'completed' AND child.created_at > utc.status_updated_at
				AND (child_utc.status IS NULL OR child_utc.status NOT IN ('completed', 'dropped'))) AS new_count
		FROM content child
		LEFT JOIN user_to_content child_utc ON child_utc.library_id = child.library_id
			AND child_utc.uri = child.uri AND child_utc.user_id = @user_id
		WHERE child.parent_id = c.id AND child.valid
	) cc ON true`
