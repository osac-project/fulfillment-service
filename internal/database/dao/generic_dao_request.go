/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package dao

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize/english"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/innabox/fulfillment-service/internal/collections"
	"github.com/innabox/fulfillment-service/internal/database"
)

// request is a common base for all DAO request types, containing shared fields.
type request[O Object] struct {
	dao     *GenericDAO[O]
	tx      database.Tx
	tenants struct {
		assignable collections.Set[string]
		def        collections.Set[string]
		visible    collections.Set[string]
	}
	sql struct {
		filter bytes.Buffer
		params []any
	}
}

// initTenants initializes the tenants for the request.
func (r *request[O]) initTenants(ctx context.Context) error {
	var err error
	assignableTenants, err := r.dao.tenancyLogic.DetermineAssignableTenants(ctx)
	if err != nil {
		return err
	}
	if assignableTenants.Empty() {
		return &ErrDenied{
			Reason: "there are no assignable tenants",
		}
	}
	defaultTenants, err := r.dao.tenancyLogic.DetermineDefaultTenants(ctx)
	if err != nil {
		return err
	}
	if defaultTenants.Empty() {
		return &ErrDenied{
			Reason: "there are no default tenants",
		}
	}
	visibleTenants, err := r.dao.tenancyLogic.DetermineVisibleTenants(ctx)
	if err != nil {
		return err
	}
	if visibleTenants.Empty() {
		return &ErrDenied{
			Reason: "there are no visible tenants",
		}
	}
	r.tenants.assignable = assignableTenants
	r.tenants.def = defaultTenants
	r.tenants.visible = visibleTenants
	return nil
}

// get retrieves a single object by its identifier. It can optionally lock the row for update.
func (r *request[O]) get(ctx context.Context, id string, lock bool) (result O, err error) {
	// Initialize the tenants:
	err = r.initTenants(ctx)
	if err != nil {
		return
	}

	// Add the id parameter:
	if id == "" {
		err = errors.New("object identifier is mandatory")
		return
	}
	r.sql.params = append(r.sql.params, id)
	r.sql.filter.WriteString("id = $1")

	// Create the where clause to filter by tenant:
	err = r.addTenancyFilter()
	if err != nil {
		return
	}

	// Create the SQL statement:
	var buffer strings.Builder
	fmt.Fprintf(
		&buffer,
		`
		select
			name,
			creation_timestamp,
			deletion_timestamp,
			finalizers,
			creators,
			tenants,
			labels,
			annotations,
			data
		from
			%s
		where
			%s
		`,
		r.dao.table,
		r.sql.filter.String(),
	)
	if lock {
		buffer.WriteString(" for update")
	}

	// Execute the SQL statement:
	sql := buffer.String()
	r.dao.logger.DebugContext(
		ctx,
		"Running SQL query",
		slog.String("sql", sql),
		slog.Any("parameters", r.sql.params),
	)
	row := r.tx.QueryRow(ctx, sql, r.sql.params...)
	var (
		name            string
		creationTs      time.Time
		deletionTs      time.Time
		finalizers      []string
		creators        []string
		tenants         []string
		labelsData      []byte
		annotationsData []byte
		data            []byte
	)
	err = row.Scan(
		&name,
		&creationTs,
		&deletionTs,
		&finalizers,
		&creators,
		&tenants,
		&labelsData,
		&annotationsData,
		&data,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = &ErrNotFound{
			ID: id,
		}
		return
	}
	if err != nil {
		return
	}

	// Prepare the object:
	object := r.cloneObject(r.newObject())
	err = r.unmarshalData(data, object)
	if err != nil {
		return
	}
	labels, err := r.unmarshalMap(labelsData)
	if err != nil {
		return
	}
	annotations, err := r.unmarshalMap(annotationsData)
	if err != nil {
		return
	}
	metadata := r.makeMetadata(makeMetadataArgs{
		creationTs:  creationTs,
		deletionTs:  deletionTs,
		finalizers:  finalizers,
		creators:    creators,
		tenants:     tenants,
		name:        name,
		labels:      labels,
		annotations: annotations,
	})
	object.SetId(id)
	r.setMetadata(object, metadata)

	// Return the result:
	result = object
	return
}

type archiveArgs struct {
	id              string
	creationTs      time.Time
	deletionTs      time.Time
	creators        []string
	tenants         []string
	name            string
	labelsData      []byte
	annotationsData []byte
	data            []byte
}

// archive moves a deleted object to the archived table and removes it from the main table.
func (r *request[O]) archive(ctx context.Context, args archiveArgs) error {
	sql := fmt.Sprintf(
		`
		insert into archived_%s (
			id,
			name,
			creation_timestamp,
			deletion_timestamp,
			creators,
			tenants,
			labels,
			annotations,
			data
		) values (
		 	$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9
		)
		`,
		r.dao.table,
	)
	_, err := r.tx.Exec(
		ctx,
		sql,
		args.id,
		args.name,
		args.creationTs,
		args.deletionTs,
		args.creators,
		args.tenants,
		args.labelsData,
		args.annotationsData,
		args.data,
	)
	if err != nil {
		return err
	}
	sql = fmt.Sprintf(`delete from %s where id = $1`, r.dao.table)
	_, err = r.tx.Exec(ctx, sql, args.id)
	return err
}

// addTenancyFilter adds a clause to restrict results to only those objects that belong to tenants the current user
// has permission to see.
func (r *request[O]) addTenancyFilter() error {
	// If the visible tenants set is universal, it means that the user has permission to see all tenants, so we don't
	// need to apply any filtering:
	if r.tenants.visible.Universal() {
		return nil
	}

	// If the visible tenants set is empty, it means that the user has no permission to see any tenants, so we can discard
	// any previous filter and return instead a one that matches nothing.
	if r.tenants.visible.Empty() {
		r.sql.filter.Reset()
		r.sql.filter.WriteString("false")
		return nil
	}

	// If the tenant set is finite, then we can add a filer that matches the tenants in the set.
	if r.tenants.visible.Finite() {
		tenants := r.tenants.visible.Inclusions()
		sort.Strings(tenants)
		r.sql.params = append(r.sql.params, tenants)
		filter := fmt.Sprintf("tenants && $%d", len(r.sql.params))
		if r.sql.filter.Len() == 0 {
			r.sql.filter.WriteString(filter)
		} else {
			previous := r.sql.filter.String()
			r.sql.filter.Reset()
			fmt.Fprintf(&r.sql.filter, "(%s) and %s", previous, filter)
		}
		return nil
	}

	// If we are here then the tenant set is infinite, and we don't know how to apply filtering for that at the
	// moment, so return an error.
	r.dao.logger.Warn(
		"Operation not permitted because visible tenant set is infinite",
		slog.Any("exclusions", r.tenants.visible.Exclusions()),
	)
	return &ErrDenied{
		Reason: "operation not permitted",
	}
}

func (r *request[O]) calculateCreators(ctx context.Context) (result []string, err error) {
	// Before returning, convert the set to a list and sort it:
	var calculated collections.Set[string]
	defer func() {
		if err != nil {
			return
		}
		result = calculated.Inclusions()
		sort.Strings(result)
	}()

	// Calculate the assigned creators:
	calculated, err = r.dao.attributionLogic.DetermineAssignedCreators(ctx)
	if err != nil {
		return
	}

	return
}

// calculateTenants calculates the tenants for the given object.
func (r *request[O]) calculateTenants(ctx context.Context, object, update O) (result []string, err error) {
	// Before returning, convert the set to a list and sort it:
	var calculated collections.Set[string]
	defer func() {
		if err != nil {
			return
		}
		result = calculated.Inclusions()
		sort.Strings(result)
	}()

	// Get the current and requested tenants from the metadata:
	current := r.getTenants(object)
	requested := r.getTenants(update)

	// Check that the user isn't requesting any tenants that aren't compabible with the visible and assignable
	// tenants.
	err = r.checkTenants(ctx, requested)
	if err != nil {
		return
	}

	// If the user is requesting an empty list of tenants then we will assume that they want to use the default
	// tenants (for new objects) or the current tenants (for updated objects):
	if requested.Empty() {
		if current.Empty() {
			requested = r.tenants.def
		} else {
			requested = current
		}
	}
	if requested.Empty() {
		err = &ErrDenied{
			Reason: "at least one tenant is required",
		}
		return
	}

	// The result should be whatever the user requested plus the tenants that are assignble and invisible:
	calculated = requested.Union(r.tenants.assignable.Intersection(r.tenants.visible.Negate()))
	return
}

// getTenants returns the tenants of the given object.
func (r *request[O]) getTenants(object O) collections.Set[string] {
	metadata := r.getMetadata(object)
	if metadata == nil {
		return collections.NewEmptySet[string]()
	}
	return collections.NewSet(metadata.GetTenants()...)
}

// checkTenants verifies that the given requested tenants are compatible with the visible and assignable tenants and
// generates an informative error message if they aren't.
func (r *request[O]) checkTenants(ctx context.Context, requested collections.Set[string]) error {
	// Make sure that the user isn't requesting tenants that are invisible to them:
	invisible := requested.Difference(r.tenants.visible)
	if !invisible.Empty() {
		ids := invisible.Inclusions()
		r.dao.logger.WarnContext(
			ctx,
			"User is trying to assign tenants that are invisible to them",
			slog.Any("visible", r.tenants.visible.Inclusions()),
			slog.Any("requested", ids),
		)
		if len(ids) == 1 {
			return &ErrDenied{
				Reason: fmt.Sprintf("tenant '%s' doesn't exist", ids[0]),
			}
		}
		sort.Strings(ids)
		for i, tenantId := range ids {
			ids[i] = fmt.Sprintf("'%s'", tenantId)
		}
		return &ErrDenied{
			Reason: fmt.Sprintf("tenants %s don't exist", english.WordSeries(ids, "and")),
		}
	}

	// Make sure that the user ins't requesting tenants that aren't assignable to them:
	unassignable := requested.Difference(r.tenants.assignable)
	if !unassignable.Empty() {
		ids := unassignable.Inclusions()
		r.dao.logger.WarnContext(
			ctx,
			"User is trying to assign tenants that are unassignable",
			slog.Any("assignable", r.tenants.assignable.Inclusions()),
			slog.Any("requested", ids),
		)
		if len(ids) == 1 {
			return &ErrDenied{
				Reason: fmt.Sprintf("tenant '%s' can't be assigned", ids[0]),
			}
		}
		sort.Strings(ids)
		for i, tenantId := range ids {
			ids[i] = fmt.Sprintf("'%s'", tenantId)
		}
		return &ErrDenied{
			Reason: fmt.Sprintf("tenants %s can't be assigned", english.WordSeries(ids, "and")),
		}
	}

	return nil
}

type makeMetadataArgs struct {
	creationTs  time.Time
	deletionTs  time.Time
	finalizers  []string
	creators    []string
	tenants     []string
	name        string
	labels      map[string]string
	annotations map[string]string
}

func (r *request[O]) makeMetadata(args makeMetadataArgs) metadataIface {
	result := r.dao.metadataTemplate.New().Interface().(metadataIface)
	result.SetName(args.name)
	if args.creationTs.Unix() != 0 {
		result.SetCreationTimestamp(timestamppb.New(args.creationTs))
	}
	if args.deletionTs.Unix() != 0 {
		result.SetDeletionTimestamp(timestamppb.New(args.deletionTs))
	}
	result.SetFinalizers(args.finalizers)
	result.SetCreators(args.creators)
	result.SetTenants(r.filterTenants(args.tenants))
	result.SetLabels(args.labels)
	result.SetAnnotations(args.annotations)
	return result
}

// filterTenants returns the intersection of the object's tenants and the user's visible tenants.
func (r *request[O]) filterTenants(tenants []string) []string {
	// If the visible tenants set is universal, it means that the user has permission to see all tenants, so we
	// don't need to filter anything:
	if r.tenants.visible.Universal() {
		return tenants
	}

	// Calculate the intersection of object tenants and visible tenants:
	result := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		if r.tenants.visible.Contains(tenant) {
			result = append(result, tenant)
		}
	}

	return result
}

func (r *request[O]) getMetadata(object O) metadataIface {
	objectReflect := object.ProtoReflect()
	if !objectReflect.Has(r.dao.metadataField) {
		return nil
	}
	return objectReflect.Get(r.dao.metadataField).Message().Interface().(metadataIface)
}

func (r *request[O]) setMetadata(object O, metadata metadataIface) {
	objectReflect := object.ProtoReflect()
	if metadata != nil {
		metadataReflect := metadata.ProtoReflect()
		objectReflect.Set(r.dao.metadataField, protoreflect.ValueOfMessage(metadataReflect))
	} else {
		objectReflect.Clear(r.dao.metadataField)
	}
}

func (r *request[O]) newObject() O {
	return r.dao.objectTemplate.New().Interface().(O)
}

func (r *request[O]) cloneObject(object O) O {
	return proto.Clone(object).(O)
}

func (r *request[O]) marshalData(object O) (result []byte, err error) {
	result, err = r.dao.jsonEncoder.Marshal(object)
	return
}

func (r *request[O]) unmarshalData(data []byte, object O) error {
	return r.dao.unmarshalOptions.Unmarshal(data, object)
}

func (r *request[O]) fireEvent(ctx context.Context, event Event) error {
	event.Table = r.dao.table
	for _, eventCallback := range r.dao.eventCallbacks {
		err := eventCallback(ctx, event)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *request[O]) getFinalizers(metadata metadataIface) []string {
	if metadata == nil {
		return []string{}
	}
	list := metadata.GetFinalizers()
	set := make(map[string]struct{}, len(list))
	for _, item := range list {
		set[item] = struct{}{}
	}
	list = make([]string, len(set))
	i := 0
	for item := range set {
		list[i] = item
		i++
	}
	sort.Strings(list)
	return list
}

func (r *request[O]) marshalMap(value map[string]string) (result []byte, err error) {
	if value == nil {
		result = []byte("{}")
		return
	}
	result, err = json.Marshal(value)
	return
}

func (r *request[O]) unmarshalMap(data []byte) (result map[string]string, err error) {
	if len(data) == 0 {
		return
	}
	var value map[string]string
	err = json.Unmarshal(data, &value)
	if err != nil {
		return
	}
	result = value
	return
}

// equivalent checks if two objects are equivalent. That means that they are equal except maybe in the creation and
// deletion timestamps.
func (r *request[O]) equivalent(x, y O) bool {
	return r.equivalentMessages(x.ProtoReflect(), y.ProtoReflect())
}

func (r *request[O]) equivalentMessages(x, y protoreflect.Message) bool {
	if x.IsValid() != y.IsValid() {
		return false
	}
	fields := x.Descriptor().Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		xPresent := x.Has(field)
		yPresent := y.Has(field)
		if xPresent != yPresent {
			return false
		}
		if !xPresent && !yPresent {
			continue
		}
		xValue := x.Get(field)
		yValue := y.Get(field)
		switch field.Name() {
		case metadataFieldName:
			if !r.equivalentMetadata(xValue.Message(), yValue.Message()) {
				return false
			}
		default:
			if !xValue.Equal(yValue) {
				return false
			}
		}
	}
	return true
}

func (r *request[O]) equivalentMetadata(x, y protoreflect.Message) bool {
	if x.IsValid() != y.IsValid() {
		return false
	}
	fields := x.Descriptor().Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if field.Name() == creationTimestampFieldName || field.Name() == deletionTimestampFieldName {
			continue
		}
		xv := x.Get(field)
		yv := y.Get(field)
		if !xv.Equal(yv) {
			return false
		}
	}
	return true
}
