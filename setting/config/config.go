package config

import (
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ConfigManager stores registered structured config modules.
type ConfigManager struct {
	configs map[string]interface{}
	mutex   sync.RWMutex
}

var GlobalConfig = NewConfigManager()

func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		configs: make(map[string]interface{}),
	}
}

func (cm *ConfigManager) Register(name string, config interface{}) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.configs[name] = config
}

func (cm *ConfigManager) Get(name string) interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.configs[name]
}

func (cm *ConfigManager) LoadFromDB(options map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for name, config := range cm.configs {
		prefix := name + "."
		configMap := make(map[string]string)

		for key, value := range options {
			if strings.HasPrefix(key, prefix) {
				configKey := strings.TrimPrefix(key, prefix)
				configMap[configKey] = value
			}
		}

		if len(configMap) == 0 {
			continue
		}
		if err := updateConfigFromMap(config, configMap); err != nil {
			common.SysError("failed to update config " + name + ": " + err.Error())
		}
	}

	return nil
}

func (cm *ConfigManager) SaveToDB(updateFunc func(key, value string) error) error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for name, config := range cm.configs {
		configMap, err := configToMap(config)
		if err != nil {
			return err
		}

		for key, value := range configMap {
			dbKey := name + "." + key
			if err := updateFunc(dbKey, value); err != nil {
				return err
			}
		}
	}

	return nil
}

func configToMap(config interface{}) (map[string]string, error) {
	result := make(map[string]string)

	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		if !fieldType.IsExported() {
			continue
		}

		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		var strValue string
		switch field.Kind() {
		case reflect.String:
			strValue = field.String()
		case reflect.Bool:
			strValue = strconv.FormatBool(field.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(field.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			strValue = strconv.FormatUint(field.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			strValue = strconv.FormatFloat(field.Float(), 'f', -1, 64)
		case reflect.Ptr:
			if field.IsNil() {
				strValue = "null"
			} else {
				bytes, err := common.Marshal(field.Interface())
				if err != nil {
					return nil, err
				}
				strValue = string(bytes)
			}
		case reflect.Map, reflect.Slice, reflect.Struct:
			bytes, err := common.Marshal(field.Interface())
			if err != nil {
				return nil, err
			}
			strValue = string(bytes)
		default:
			continue
		}

		result[key] = strValue
	}

	return result, nil
}

func updateConfigFromMap(config interface{}, configMap map[string]string) error {
	val := reflect.ValueOf(config)
	if val.Kind() != reflect.Ptr {
		return nil
	}
	val = val.Elem()

	if val.Kind() != reflect.Struct {
		return nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		if !fieldType.IsExported() {
			continue
		}

		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		strValue, ok := configMap[key]
		if !ok || !field.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(strValue)
		case reflect.Bool:
			boolValue, err := strconv.ParseBool(strValue)
			if err != nil {
				continue
			}
			field.SetBool(boolValue)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			intValue, err := strconv.ParseInt(strValue, 10, 64)
			if err != nil {
				floatValue, floatErr := strconv.ParseFloat(strValue, 64)
				if floatErr != nil {
					continue
				}
				intValue = int64(floatValue)
			}
			field.SetInt(intValue)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			uintValue, err := strconv.ParseUint(strValue, 10, 64)
			if err != nil {
				floatValue, floatErr := strconv.ParseFloat(strValue, 64)
				if floatErr != nil || floatValue < 0 {
					continue
				}
				uintValue = uint64(floatValue)
			}
			field.SetUint(uintValue)
		case reflect.Float32, reflect.Float64:
			floatValue, err := strconv.ParseFloat(strValue, 64)
			if err != nil {
				continue
			}
			field.SetFloat(floatValue)
		case reflect.Ptr:
			if strValue == "null" {
				field.Set(reflect.Zero(field.Type()))
				continue
			}
			if field.IsNil() {
				field.Set(reflect.New(field.Type().Elem()))
			}
			if err := common.Unmarshal([]byte(strValue), field.Interface()); err != nil {
				continue
			}
		case reflect.Map, reflect.Slice, reflect.Struct:
			// Decode into a fresh value so {} fully replaces existing maps.
			newValue := reflect.New(field.Type())
			if err := common.Unmarshal([]byte(strValue), newValue.Interface()); err != nil {
				continue
			}
			field.Set(newValue.Elem())
		}
	}

	return nil
}

func ConfigToMap(config interface{}) (map[string]string, error) {
	return configToMap(config)
}

func UpdateConfigFromMap(config interface{}, configMap map[string]string) error {
	return updateConfigFromMap(config, configMap)
}

func (cm *ConfigManager) ExportAllConfigs() map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	result := make(map[string]string)

	for name, cfg := range cm.configs {
		configMap, err := ConfigToMap(cfg)
		if err != nil {
			continue
		}

		for key, value := range configMap {
			result[name+"."+key] = value
		}
	}

	return result
}
