# Anthropic API 兼容

## 1.请求地址
https://api.xiaomimimo.com/anthropic/v1/messages

## 2.请求头
接口支持以下两种认证方式，请选择其中一种添加到请求头中：

方式一：api-key 字段认证，格式：

api-key: $MIMO_API_KEY
Content-Type: application/json

方式二：Authorization: Bearer 认证，格式：

Authorization: Bearer $MIMO_API_KEY
Content-Type: application/json

## 3.请求体

messages array 必选
输入消息列表。
每个消息必须包含 role 和 content 字段。您可以指定单个用户角色消息，或包含多个用户和助手消息。如果最后一条消息使用助手角色，响应内容将直接从该消息的内容继续，这可以用来约束模型的响应。

	messages.role string 必选
	消息的角色。
	可选值：user，assistant

	messages.content string | array 必选

		Text content · string
		消息的文本内容。

		Array of content parts · array
		一个包含多个具有特定类型的内容部分的数组。例如，文本、图像、工具使用、工具结果和思考。
		>仅 mimo-v2.5，mimo-v2-omni 模型支持图像输入。

			Text · object
				messages.content.text string 必选
				文本块的内容。
				最小值：1

				messages.content.type string 必选
				内容的类型。
				可选值：text
				
			Image · object
				messages.content.source object 必选
				图像数据通过 URL 或 Base64 提供。
				
					Base64ImageSource · object
						messages.content.source.data string 必选
						Base64 编码的图像数据。
						
						messages.content.source.media_type string 必选
						媒体类型。
						可选值：image/jpeg，image/png，image/gif，image/webp，image/bmp
						
						messages.content.source.type string 必选
						图像源类型。
						可选值：base64
						
					URLImageSource · object
						messages.content.source.url string 必选
						图像的 URL。
						
						messages.content.source.type string 必选
						图像源类型。
						可选值：url
						
				messages.content.type string 必选
				内容的类型。
				可选值：image
					
			Tool use · object
				messages.content.id string 必选
				工具使用的唯一标识符。
				
				messages.content.input object 必选
				使用工具时传入的参数对象。
				
				messages.content.name string 必选
				工具名称。
				
				messages.content.type string 必选
				内容的类型。
				可选值：tool_use
					
			Tool result · object		
				messages.content.tool_use_id string 必选
				与本次结果对应的 tool_use 的 ID。
					
				messages.content.content string | array
				工具执行后返回的结果。
				
					Text content · string
					消息的文本内容。
					
					Array of content parts · array
					一个包含多个具有特定类型的内容部分的数组。例如，文本和图像。
					
						Text · object
							messages.content.content.text string 必选
							文本块的内容。
							
							messages.content.content.type string 必选
							内容的类型。
							可选值：text
							
						Image · object
							messages.content.content.source object 必选
							图像数据通过 URL 或 Base64 提供。
							
								Base64ImageSource · object
									messages.content.content.source.data string 必选
									Base64 编码的图像数据。
									
									messages.content.content.source.media_type string 必选
									媒体类型。
									可选值：image/jpeg，image/png，image/gif，image/webp，image/bmp
						
									messages.content.content.source.type string 必选
									图像源类型。
									可选值：base64
									
								URLImageSource · object
									messages.content.content.source.url string 必选
									图像的 URL。
									
									messages.content.content.source.type string 必选
									图像源类型。
									可选值：url
							
							messages.content.content.type string 必选
							内容的类型。
							可选值：image
							
				messages.content.is_error boolean			
				
				messages.content.type string 必选
				内容的类型。
				可选值：tool_result
				
			Thinking · object
				messages.content.signature string
				思考块的签名。
				
				messages.content.thinking string 必选
				思考内容。
				
				messages.content.type string 必选
				内容的类型。
				可选值：thinking
				
model string 必选
使用的模型名称。
可选值：mimo-v2.5-pro，mimo-v2.5，mimo-v2-pro，mimo-v2-omni，mimo-v2-flash

max_tokens integer
停止前生成的最大 token 数。
请注意，我们的模型可能在达到此最大值之前就停止。此参数仅指定要生成的绝对最大 token 数。
mimo-v2-flash 的默认值 65536
mimo-v2.5-pro，mimo-v2-pro 的默认值 131072
mimo-v2.5，mimo-v2-omni 的默认值为 32768
所需范围：[1, 131072]

stop_sequences array
使模型停止生成的自定义文本序列。
我们的模型通常会在自然完成一轮对话后停止，这将导致响应的 stop_reason 为 end_turn。
如果您希望模型在遇到自定义文本字符串时停止生成，可以使用 stop_sequences 参数。

stream boolean
默认值: false
是否以流式输出方式回复。	

system string | array
系统提示词是向模型提供上下文与指令的一种方式，例如为模型指定特定目标或角色。

	Text content · string
	系统提示词的内容。
	
	Array of content parts · array
		system.text string 必选
		文本内容。
		最小长度：1
		
		system.type string 必选
		内容的类型。
		可选值：text

temperature number
采样温度，控制模型生成文本的多样性。
temperature 越高，生成的文本更多样，反之，生成的文本更确定。
mimo-v2-flash 默认值为 0.3
mimo-v2.5-pro，mimo-v2.5，mimo-v2-pro，mimo-v2-omni 默认值为 1.0
所需范围：[0, 1.5]	

thinking object
>启用模型扩展思维的配置。
>注意：在思考模式下的多轮工具调用过程中，模型会在返回 tool_use 内容块的同时返回 thinking 内容块。若要继续对话，建议在后续每次请求的 messages 数组中保留所有历史 thinking 内容块，以获得最佳表现。

	thinking.type string 必选
	mimo-v2-flash 默认值为 disabled
	mimo-v2.5-pro，mimo-v2.5，mimo-v2-pro，mimo-v2-omni 默认值为 enabled
	可选值：enabled，disabled

tool_choice object
控制模型如何使用提供的工具。
	tool_choice.type string 必选
	auto 意味着模型将自动决定是否使用工具。
	>注意：当 type 传入非 auto 值时，后端会默认移除该字段，模型响应行为仍等同于 auto 模式（该逻辑保留调整的可能性）。
	可选值：auto
	
	tool_choice.disable_parallel_tool_use boolean
	默认值: false
	是否禁用并行工具使用。
	如果设置为 true：
	当类型为 auto 时，模型将输出至多一个工具使用。
	
tools array
模型可能会使用的工具的定义。
如果在 API 请求中包含工具，则模型可能会返回 tool_use 内容块，表示模型对这些工具的使用。您可以使用模型生成的工具输入运行这些工具，然后选择性地返回结果给模型，使用 tool_result 内容块。
>注意：在思考模式下的多轮工具调用过程中，模型会在返回 tool_use 内容块的同时返回 thinking 内容块。若要继续对话，建议在后续每次请求的 messages 数组中保留所有历史 thinking 内容块，以获得最佳表现。	
工具定义包括：
name：工具的名称。
description：可选，但强烈推荐填写工具描述。
input_schema：工具输入形状的 JSON 模式，模型将在 tool_use 输出内容块中生成。

	tools.name string 必选
	工具名称。
	模型将通过它调用该工具，并是在 tool_use 块中使用的名称。
	
	tools.description string
	工具的描述。
	工具描述应尽可能详细。模型关于工具是什么以及如何使用的信息越多，执行表现就越好。您可以使用自然语言描述来强化工具输入 JSON 模式中的重要信息。
	
	tools.type string
	可选值：custom
	
	tools.input_schema object 必选
	工具输入形状的 JSON 模式，模型将在 tool_use 输出内容块中生成。

		tools.input_schema.type string 必选
		input_schema 的类型，仅为 object。
		可选值：object
		
		tools.input_schema.properties object | null
		工具输入的属性。
		
		tools.input_schema.required array | null
		工具输入中必须包含的属性列表。
		
top_p number 默认值: 0.95
启用核采样。
在核采样机制中，我们会按概率从高到低的顺序，为生成每个后续 token 的所有候选结果计算累积概率分布，当累积概率达到 top_p 参数指定的阈值时，便会截断后续候选。请注意，你应仅调整 temperature 或 top_p 二者其一，不可同时修改。
此采样方式仅建议用于高级使用场景。通常情况下，你只需调整 temperature 参数即可满足需求。
所需范围：[0.01, 1.0]		


## 非流式响应
id string
该对话的唯一标识符。ID 的格式和长度可能会随时间而变化。

type string
对象类型，对于 Messages 始终为 message。

role string
生成消息的会话角色，始终为 assistant。

content array
模型生成的内容，由多个内容块组成。每个内容块都有一个 type。
	Text · object
		content.text string
		文本内容。
		
		content.type string
		内容的类型。
		可选值：text

	Thinking · object
		content.signature string
		思考块的签名。
		
		content.thinking string
		思考内容。
		
		content.type string
		内容的类型。
		可选值：thinking

	Tool use · object
		content.id string
		工具使用的唯一标识符。
		
		content.input object
		使用工具时传入的参数对象。
		
		content.name string
		工具名称。
		
		content.type string
		内容的类型。
		可选值：tool_use
		
model string
使用的模型名称。

stop_reason string
消息完成的原因。
其取值可能为以下之一：
end_turn：模型达到自然停止点。
max_tokens：超过请求的 max_tokens 或模型的最大限制。
tool_use：模型调用了一个或多个工具。
content_filter：内容因触发过滤策略而被拦截。
repetition_truncation：模型检测到了复读。
可选值：end_turn，max_tokens，tool_use，content_filter，repetition_truncation

usage object
计费和限流相关的使用量统计。		
	usage.input_tokens integer
	使用的输入 token 数量。
	
	usage.output_tokens integer
	使用的输出 token 数量。
	
	usage.cache_read_input_tokens integer | null
	从缓存读取的输入 token 数量。
	
## 流式响应

SSE.event string
描述的事件类型标识的字符串。
可选值：message_start，content_block_start，content_block_delta，content_block_stop，message_delta，message_stop

type string
每个服务器发送的事件包括一个命名事件类型和关联的 JSON 数据。
可选值：message_start，content_block_start，content_block_delta，content_block_stop，message_delta，message_stop

message object
响应消息。

	message.id string
	消息 ID。
	
	message.type string
	可选值：message
	
	message.role string
	可选值：assistant
	
	message.model string
	模型名称。
	
	message.content array
	消息中的内容块数组。
	
	message.stop_reason string | null
	消息完成的原因。
	
index integer
内容块在消息中的位置。

content_block object
开始的内容块。	
	Text · object
		content_block.type string
		文本内容块的头部；实际文本通过后续 delta 事件到达。
		可选值：text
		
		content_block.text string
		开头通常为空字符串；文本通过 text_delta 类型的 content_block_delta 事件附加。

	Thinking · object
		content_block.type string
		思考内容块的头部；实际思考内容通过后续 delta 事件到达。
		可选值：thinking
		
		content_block.thinking string
		开头通常为空字符串；思考内容通过类型为 thinking_delta 的 content_block_delta 事件追加。

	Tool use · object
		content_block.type string
		可选值：tool_use
		
		content_block.id string
		工具使用的唯一标识符。
		
		content_block.name string
		工具名称。
		
		content_block.input object
		使用工具时传入的参数对象。

delta object
实际响应内容。
	Content block delta · object
		delta.type string
		可选值：text_delta，thinking_delta，input_json_delta
		
		delta.text string
		增量数据的文本部分。
		
		delta.thinking string
		增量数据的思考部分。
		
		delta.partial_json string
		JSON 片段字符串。按到达顺序连接片段以形成完整的输入 JSON，然后解析。

	Message delta · object
		delta.stop_reason string | null
		消息完成的原因。
		可选值: end_turn，max_tokens，tool_use，content_filter，repetition_truncation
	
usage object | null
计费和限流相关的使用量统计。
	usage.input_tokens integer
	使用的输入 token 数量。
	
	usage.output_tokens integer
	使用的输出 token 数量。
	
	usage.cache_read_input_tokens integer | null
	从缓存读取的输入 token 数量。
	
## 调用示例

	基础调用
	
		- 请求：
		curl --location --request POST 'https://api.xiaomimimo.com/anthropic/v1/messages' \
		--header "api-key: $MIMO_API_KEY" \
		--header "Content-Type: application/json" \
		--data-raw '{
			"model": "mimo-v2.5-pro",
			"max_tokens": 1024,
			"system": "You are MiMo, an AI assistant developed by Xiaomi. Today is date: Tuesday, December 16, 2025. Your knowledge cutoff date is December 2024.",
			"messages": [
				{
					"role": "user",
					"content": [
						{
							"type": "text",
							"text": "please introduce yourself"
						}
					]
				}
			],
			"top_p": 0.95,
			"stream": false,
			"temperature": 1.0,
			"stop_sequences": null,
			"thinking": {
				"type": "disabled"
			}
		}'
		- 响应：
		{
			"id": "b966dbcad38c48b59d16d8c1f313681b",
			"type": "message",
			"role": "assistant",
			"model": "mimo-v2.5-pro",
			"stop_reason": "end_turn",
			"content": [
				{
					"type": "text",
					"text": "Hello! I'm MiMo, an AI assistant developed by Xiaomi. I'm here to help answer your questions, provide information, or assist with various tasks. My knowledge is up to date until December 2024. How can I help you today?"
				}
			],
			"usage": {
				"input_tokens": 57,
				"output_tokens": 54
			}
		}

	流式响应
		- 请求：
		curl --location --request POST 'https://api.xiaomimimo.com/anthropic/v1/messages' \
		--header "api-key: $MIMO_API_KEY" \
		--header "Content-Type: application/json" \
		--data-raw '{
			"model": "mimo-v2.5-pro",
			"max_tokens": 1024,
			"stream": true,
			"system": "You are MiMo, an AI assistant developed by Xiaomi. Today is date: Tuesday, December 16, 2025. Your knowledge cutoff date is December 2024.",
			"messages": [
				{
					"role": "user",
					"content": [
						{
							"type": "text",
							"text": "please introduce yourself"
						}
					]
				}
			]
		}'
		- 响应：
		event: message_start
		data: {"type":"message_start","message":{"id":"msg_ef7206c645d4400196c80dd3","type":"message","role":"assistant","model":"mimo-v2.5-pro","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}

		event: content_block_start
		data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

		event: content_block_delta
		data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"Okay, the user"},"index":0}

		event: content_block_delta
		data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":" is asking"},"index":0}

		...

		event: content_block_delta
		data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" you!"},"index":1}

		event: content_block_stop
		data: {"type":"content_block_stop","index":1}

		event: message_delta
		data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":55,"output_tokens":143}}

		event: message_stop
		data: {"type":"message_stop"}

	函数调用
		- 请求：
		curl --location --request POST 'https://api.xiaomimimo.com/anthropic/v1/messages' \
		--header "api-key: $MIMO_API_KEY" \
		--header "Content-Type: application/json" \
		--data-raw '{
			"model": "mimo-v2.5-pro",
			"max_tokens": 1024,
			"system": "You are MiMo, an AI assistant developed by Xiaomi. Today is date: Tuesday, December 16, 2025. Your knowledge cutoff date is December 2024.",
			"messages": [
				{
					"role": "user",
					"content": "What is the weather like in Beijing today?"
				}
			],
			"tools": [
				{
					"name": "get_weather",
					"description": "Get the current weather of the specified location",
					"type": "custom",
					"input_schema": {
						"type": "object",
						"properties": {
							"location": {
								"type": "string",
								"description": "City name, e.g., Beijing"
							}
						},
						"required": [
							"location"
						]
					}
				}
			],
			"tool_choice": {
				"type": "auto"
			}
		}'
		- 响应：
		{
			"id": "723132f181d94dfea387a6149847b235",
			"type": "message",
			"role": "assistant",
			"model": "mimo-v2.5-pro",
			"stop_reason": "tool_use",
			"content": [
				{
					"type": "thinking",
					"thinking": "The user wants to know the current weather in Beijing. I have the get_weather tool available that can provide this information. I need to call it with the location parameter set to \\\"Beijing\\\". Let me do that.",
					"signature": ""
				},
				{
					"type": "tool_use",
					"id": "call_43c1245f5c714ae89a6b80ef",
					"name": "get_weather",
					"input": {
						"location": "Beijing"
					}
				}
			],
			"usage": {
				"input_tokens": 310,
				"output_tokens": 68
			}
		}

	图像输入
		- 请求：
		curl --location --request POST 'https://api.xiaomimimo.com/anthropic/v1/messages' \
		--header "api-key: $MIMO_API_KEY" \
		--header "Content-Type: application/json" \
		--data-raw '{
			"model": "mimo-v2.5",
			"max_tokens": 1024,
			"system": "You are MiMo, an AI assistant developed by Xiaomi. Today is date: Tuesday, December 16, 2025. Your knowledge cutoff date is December 2024.",
			"messages": [
				{
					"role": "user",
					"content": [
						{
							"type": "image",
							"source": {
								"type": "url",
								"url": "https://example-files.cnbj1.mi-fds.com/example-files/image/image_example.png"
							}
						},
						{
							"type": "text",
							"text": "please describe the content of the image"
						}
					]
				}
			]
		}'
		
		- 响应：
		{
			"id": "d4a90bc563ac47d0bbda1bb21a4f909a",
			"type": "message",
			"role": "assistant",
			"model": "mimo-v2.5",
			"stop_reason": "end_turn",
			"content": [
				{
					"type": "text",
					"text": "This is an ethereal, serene old-growth forest scene centered on a shallow, sun-dappled stream:\n\n1.  **Foreground**: Smooth, moss-draped stones line the edge of a clear, gently flowing stream that catches the sunlight, creating a shimmering path across its surface. On the left, a large, vibrant green fern with intricate, feathery fronds rests on moss-covered forest floor, with thick, plush moss blanketing the rocks and ground around it.\n2.  **Midground**: The stream winds deeper into the woods, bordered by dense, low-growing green shrubs and more mossy, verdant undergrowth. The water glows with reflected sunlight, leading the eye deeper into the scene.\n3.  **Background**: Massive, ancient trees with thick, gnarled, sprawling trunks and twisted branches dominate the space. Soft, misty air fills the forest, and dramatic sunbeams, crepuscular rays, cut through the canopy, illuminating tiny floating particles like dust or pollen, which adds to the magical, tranquil atmosphere. The mist softens the distant trees, creating a sense of quiet, deep wilderness.\n\nThe overall feeling is one of peaceful, untouched natural sanctuary, full of lush, rich greenery and the quiet, gentle energy of a sunlit, misty forest."
				},
				{
					"type": "thinking",
					"thinking": "Got it, let's break down this image step by step. First, the scene is a lush, misty old-growth forest with a small stream running through it.\n\nStart with the foreground: smooth, moss-covered stones line the edge of a shallow, clear stream that reflects the sunlight. On the left, a large, vibrant green fern with detailed fronds sits on mossy ground, with moss also covering the rocks and the forest floor.\n\nThen the midground: the stream winds deeper into the forest, flanked by dense, low green shrubs and more mossy growth. The water catches the light, creating a shimmering path.\n\nThe background is dominated by massive, ancient trees with thick, gnarled trunks and sprawling, twisted branches. Sunbeams (crepuscular rays) cut through the misty, hazy air of the forest, filtering through the tree canopy, and you can see tiny particles (like dust or pollen) floating in the light beams, adding to the magical, serene atmosphere. The mist softens the background trees, giving a sense of depth and quiet, tranquil wilderness.\n\nThe overall mood is peaceful, ethereal, and deeply natural—like a hidden, untouched forest sanctuary, full of lush greenery, dappled sunlight, and the quiet flow of the stream.",
					"signature": ""
				}
			],
			"usage": {
				"input_tokens": 4,
				"output_tokens": 540,
				"cache_read_input_tokens": 1081
			}
		}

	深度思考
		- 请求：
		curl --location --request POST 'https://api.xiaomimimo.com/anthropic/v1/messages' \
		--header "api-key: $MIMO_API_KEY" \
		--header "Content-Type: application/json" \
		--data-raw '{
			"model": "mimo-v2.5-pro",
			"max_tokens": 1024,
			"system": "You are MiMo, an AI assistant developed by Xiaomi. Today is date: Tuesday, December 16, 2025. Your knowledge cutoff date is December 2024.",
			"messages": [
				{
					"role": "user",
					"content": [
						{
							"type": "text",
							"text": "Introduce machine learning in three sentences."
						}
					]
				}
			],
			"thinking": {
				"type": "enabled"
			}
		}'
		- 响应：
		{
			"id": "e726e51203b241a6b2435ec52c8a8ac6",
			"type": "message",
			"role": "assistant",
			"model": "mimo-v2.5-pro",
			"stop_reason": "end_turn",
			"content": [
				{
					"type": "text",
					"text": "Machine learning is a branch of artificial intelligence where algorithms learn from data to identify patterns and make predictions or decisions. It improves automatically through experience without being explicitly programmed for each task. By analyzing large datasets, these models can adapt and enhance their performance over time."
				},
				{
					"type": "thinking",
					"thinking": "Okay, the user is asking for a three-sentence introduction to machine learning, so I need to keep it concise and clear. No deep needs here, just a straightforward explanation. As MiMo from Xiaomi, I'll align with helpful and accurate info without extra details. Key points: define ML as algorithms learning from data, mention pattern recognition and predictions, and note how it improves with more data. This makes it educational and engaging without overwhelming.",
					"signature": ""
				}
			],
			"usage": {
				"input_tokens": 60,
				"output_tokens": 143
			}
		}
