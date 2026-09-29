// Never display backend exception text or credentials on the public monitor page.
export function monitorFailureMessage(code, t) {
  const screenshot = /^screenshot_http_(\d{3})$/.exec(code || '');
  if (screenshot)
    return t('截图服务返回 HTTP {{status}}', { status: screenshot[1] });
  const http = /^HTTP (\d{3})$/.exec(code || '');
  if (http) return t('模型接口返回 HTTP {{status}}', { status: http[1] });
  switch (code) {
    case 'local_output_failed':
      return t('本地输出保存失败，请检查目录权限');
    case 'html_preview_failed':
      return t('HTML 预览生成失败');
    case 'r2_upload_failed':
      return t('测试文件上传 R2 失败，本地文件已保留');
    case 'probe_timeout':
      return t('模型请求超时');
    case 'probe_cancelled':
      return t('模型请求已取消');
    case 'probe_connection_failed':
      return t('无法连接模型接口');
    case 'sse_read_failed':
      return t('模型流式响应读取失败');
    case 'expected_sse_response':
    case 'invalid_sse_json':
      return t('模型响应格式不正确');
    case 'response_not_completed':
    case 'response_incomplete':
    case 'response.incomplete':
      return t('模型响应未完整结束');
    case 'response.failed':
    case 'messages_error':
    case 'error':
      return t('模型返回错误');
    case 'response_too_large':
    case 'svg_too_large':
      return t('模型响应超过大小限制');
    case 'svg_missing':
      return t('未返回完整 SVG');
    case 'svg_invalid_xml':
      return t('SVG 格式不正确');
    case 'svg_unsupported_element':
      return t('SVG 包含不支持的元素或命名空间');
    case 'svg_unsafe_attribute':
    case 'svg_external_resource':
    case 'svg_unsafe_content':
      return t('SVG 包含不允许的属性、内容或外部资源');
    case 'screenshot_connection_failed':
      return t('无法连接截图服务，请检查服务是否启动');
    case 'screenshot_timeout':
      return t('截图超时');
    case 'screenshot_read_failed':
    case 'screenshot_invalid_png':
      return t('截图结果读取失败或不是 PNG');
    case 'r2_config_failed':
      return t('R2 配置无效');
    case 'r2_png_upload_failed':
      return t('PNG 上传到 R2 失败');
    case 'r2_html_upload_failed':
      return t('HTML 上传到 R2 失败');
    case 'r2_retention_failed':
      return t('R2 历史作品清理失败');
    case 'artwork_index_failed':
      return t('作品索引写入 Redis 失败');
    case 'artwork_storage_or_screenshot_failed':
      return t('SVG 校验、截图或存储失败（旧记录）');
    default:
      return t('检测失败，请查看后台日志');
  }
}
