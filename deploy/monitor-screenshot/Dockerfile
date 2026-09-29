FROM mcr.microsoft.com/playwright:v1.51.1-noble
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev --ignore-scripts
COPY server.mjs ./
USER pwuser
EXPOSE 3011
CMD ["node", "server.mjs"]
